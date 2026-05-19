package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"
	"go.uber.org/zap"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"
)

type RuntimeInbound struct {
	MappingID string `json:"mappingId"`
	Tag       string `json:"tag"`
	Listen    string `json:"listen"`
	Outbound  string `json:"outbound"`
}

type RuntimeInboundFailure struct {
	MappingID string `json:"mappingId"`
	Tag       string `json:"tag"`
	Listen    string `json:"listen"`
	Error     string `json:"error"`
}

type RuntimeExcludedNode struct {
	MappingID string `json:"mappingId"`
	NodeID    string `json:"nodeId"`
	NodeName  string `json:"nodeName"`
	Tag       string `json:"tag"`
	Error     string `json:"error"`
}

type RuntimeRouteNode struct {
	NodeID            string    `json:"nodeId"`
	NodeName          string    `json:"nodeName,omitempty"`
	NodeTag           string    `json:"nodeTag"`
	Kind              string    `json:"kind"`
	Selected          bool      `json:"selected"`
	Available         bool      `json:"available"`
	LatencyCandidate  bool      `json:"latencyCandidate"`
	LatencyFallback   bool      `json:"latencyFallback"`
	LatencySlowCount  int       `json:"latencySlowCount"`
	LatencyMs         int64     `json:"latencyMs"`
	Error             string    `json:"error,omitempty"`
	LastCheckedAt     time.Time `json:"lastCheckedAt,omitempty"`
	LastSuccessAt     time.Time `json:"lastSuccessAt,omitempty"`
	ProbeStartedAt    time.Time `json:"probeStartedAt,omitempty"`
	ProbeRunning      bool      `json:"probeRunning"`
	ProbeFailureCount int       `json:"probeFailureCount"`
}

type RuntimeRoute struct {
	MappingID         string             `json:"mappingId"`
	GroupTag          string             `json:"groupTag"`
	Strategy          string             `json:"strategy"`
	SelectedMemberID  string             `json:"selectedMemberId,omitempty"`
	SelectedMemberTag string             `json:"selectedMemberTag,omitempty"`
	SelectedNodeID    string             `json:"selectedNodeId,omitempty"`
	SelectedNodeName  string             `json:"selectedNodeName,omitempty"`
	SelectedNodeTag   string             `json:"selectedNodeTag,omitempty"`
	SelectedNodeKind  string             `json:"selectedNodeKind,omitempty"`
	ProbeRunning      bool               `json:"probeRunning"`
	RuntimeStarted    bool               `json:"runtimeStarted"`
	LastProbeAt       time.Time          `json:"lastProbeAt,omitempty"`
	NextProbeAt       time.Time          `json:"nextProbeAt,omitempty"`
	Nodes             []RuntimeRouteNode `json:"nodes"`
}

type RuntimeStatus struct {
	Running       bool                    `json:"running"`
	State         string                  `json:"state"`
	Error         string                  `json:"error,omitempty"`
	Inbounds      []RuntimeInbound        `json:"inbounds"`
	Failures      []RuntimeInboundFailure `json:"failures"`
	ExcludedNodes []RuntimeExcludedNode   `json:"excludedNodes"`
	Routes        []RuntimeRoute          `json:"routes"`
	UpdatedAt     time.Time               `json:"updatedAt"`
}

type runtimeInstance struct {
	core       *singboxcore.Core
	inbound    RuntimeInbound
	inboundKey string
}

type runtimeManager struct {
	mu        sync.Mutex
	instances map[string]*runtimeInstance
	status    RuntimeStatus
}

type nodeBuildError struct {
	node *tables.ProxyNodeTable
	err  error
}

type dynamicMemberError struct {
	member dynamicMemberPlan
	err    error
}

type dynamicRuntimePlan struct {
	options       option.Options
	inbound       RuntimeInbound
	inboundKey    string
	groups        []dynamicGroupPlan
	outbounds     map[string]option.Outbound
	outboundNodes map[string]*tables.ProxyNodeTable
}

type dynamicGroupPlan struct {
	tag      string
	policy   singboxcore.Policy
	members  []dynamicMemberPlan
	selected string
}

type dynamicMemberPlan struct {
	id        string
	tag       string
	outbound  option.Outbound
	outbounds []option.Outbound
	builtin   bool
}

func (m dynamicMemberPlan) outboundTags() []string {
	tags := make([]string, 0, len(m.outbounds)+1)
	for _, outbound := range m.outbounds {
		if outbound.Tag != "" {
			tags = append(tags, outbound.Tag)
		}
	}
	if !containsString(tags, m.tag) {
		tags = append(tags, m.tag)
	}
	return uniqueNonEmpty(tags)
}

func (err nodeBuildError) Error() string {
	if err.err == nil {
		return ""
	}
	if err.node == nil {
		return err.err.Error()
	}
	return fmt.Sprintf("节点 %s 配置无效: %v", err.node.Name, err.err)
}

func (err nodeBuildError) Unwrap() error {
	return err.err
}

func asNodeBuildError(err error) (nodeBuildError, bool) {
	var buildErr nodeBuildError
	if errors.As(err, &buildErr) && buildErr.node != nil {
		return buildErr, true
	}
	return nodeBuildError{}, false
}

func (err dynamicMemberError) Error() string {
	if err.err == nil {
		return ""
	}
	name := firstNonEmpty(err.member.id, err.member.tag)
	if name == "" {
		return err.err.Error()
	}
	return fmt.Sprintf("成员 %s 初始化失败: %v", name, err.err)
}

func (err dynamicMemberError) Unwrap() error {
	return err.err
}

func asDynamicMemberError(err error) (dynamicMemberError, bool) {
	var memberErr dynamicMemberError
	if errors.As(err, &memberErr) && (memberErr.member.id != "" || memberErr.member.tag != "") {
		return memberErr, true
	}
	return dynamicMemberError{}, false
}

var singBoxRuntime = &runtimeManager{
	instances: map[string]*runtimeInstance{},
	status: RuntimeStatus{
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	},
}

func RuntimeStatusGet() RuntimeStatus {
	singBoxRuntime.mu.Lock()
	status := singBoxRuntime.status
	status.Inbounds = append([]RuntimeInbound{}, singBoxRuntime.status.Inbounds...)
	status.Failures = append([]RuntimeInboundFailure{}, singBoxRuntime.status.Failures...)
	status.ExcludedNodes = append([]RuntimeExcludedNode{}, singBoxRuntime.status.ExcludedNodes...)
	status.Routes = runtimeRoutesLocked()
	status.UpdatedAt = time.Now()
	singBoxRuntime.mu.Unlock()

	status.Routes = hydrateRuntimeRouteNames(context.Background(), status.Routes)
	return status
}

func RuntimeReload(ctx context.Context) (RuntimeStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	mappings, err := enabledRuntimeMappings(ctx, nil)
	if err != nil {
		status := setRuntimeError(err)
		return status, err
	}

	oldInstances := replaceRuntimeInstances(RuntimeStatus{
		Running:   false,
		State:     "reloading",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	})

	if closeErr := closeRuntimeInstances(oldInstances); closeErr != nil {
		utils.Logger.Warn("关闭旧 sing-box 实例失败", zap.Error(closeErr))
	}

	instances := make(map[string]*runtimeInstance, len(mappings))
	inbounds := make([]RuntimeInbound, 0, len(mappings))
	failures := make([]RuntimeInboundFailure, 0)
	excludedNodes := make([]RuntimeExcludedNode, 0)

	for _, mapping := range mappings {
		instance, inbound, mappingExcludedNodes, failure := createRuntimeMappingInstance(ctx, mapping)
		excludedNodes = append(excludedNodes, mappingExcludedNodes...)
		if failure != nil {
			failures = append(failures, *failure)
			continue
		}

		instances[mapping.ID] = instance
		inbounds = append(inbounds, inbound)
	}

	setRuntimeInstances(
		instances,
		runtimeStatusFromResults(len(mappings), inbounds, failures, excludedNodes),
	)
	return RuntimeStatusGet(), nil
}

func RuntimeSyncMapping(ctx context.Context, mappingID string) (RuntimeStatus, error) {
	mappingID = strings.TrimSpace(mappingID)
	if mappingID == "" {
		return RuntimeStatusGet(), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	mapping, err := MappingGet(ctx, nil, mappingID)
	if errors.Is(err, ErrMappingNotFound) {
		status, removeErr := RuntimeRemoveMapping(mappingID)
		return status, removeErr
	}
	if err != nil {
		return RuntimeStatusGet(), err
	}
	if !mapping.Enabled {
		return RuntimeRemoveMapping(mapping.ID)
	}

	if updated, status, err := syncRuntimeMappingDynamic(ctx, mapping); updated {
		if err != nil {
			return status, err
		}
		return RuntimeStatusGet(), nil
	}

	oldInstance := detachRuntimeMapping(mapping.ID)
	if closeErr := closeRuntimeInstance(mapping.ID, oldInstance); closeErr != nil {
		utils.Logger.Warn("关闭旧 sing-box 映射实例失败", zap.String("mappingId", mapping.ID), zap.Error(closeErr))
	}

	instance, inbound, excludedNodes, failure := createRuntimeMappingInstance(ctx, mapping)
	if failure != nil {
		setRuntimeMappingFailure(mapping.ID, *failure, excludedNodes)
		return RuntimeStatusGet(), nil
	}
	setRuntimeMappingInstance(mapping.ID, instance, inbound, excludedNodes)
	return RuntimeStatusGet(), nil
}

func RuntimeSyncMappings(ctx context.Context, mappingIDs []string) (RuntimeStatus, error) {
	mappingIDs = uniqueNonEmpty(mappingIDs)
	if len(mappingIDs) == 0 {
		return RuntimeStatusGet(), nil
	}

	var joined error
	status := RuntimeStatusGet()
	for _, mappingID := range mappingIDs {
		nextStatus, err := RuntimeSyncMapping(ctx, mappingID)
		status = nextStatus
		if err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return status, joined
}

func RuntimeRemoveMapping(mappingID string) (RuntimeStatus, error) {
	mappingID = strings.TrimSpace(mappingID)
	if mappingID == "" {
		return RuntimeStatusGet(), nil
	}

	oldInstance := detachRuntimeMapping(mappingID)
	err := closeRuntimeInstance(mappingID, oldInstance)
	return RuntimeStatusGet(), err
}

func RuntimeAffectedMappingIDsByNodes(ctx context.Context, nodeIDs []string) ([]string, error) {
	return runtimeAffectedMappingIDsByNodes(ctx, nil, nodeIDs)
}

func RuntimeAffectedMappingIDsByGroups(ctx context.Context, groupIDs []string) ([]string, error) {
	return runtimeAffectedMappingIDsByGroups(ctx, nil, groupIDs)
}

func RuntimeAffectedMappingIDsBySubscription(ctx context.Context, subscriptionID string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return []string{}, nil
	}

	tx := model.GetTx(nil).WithContext(ctx)
	var subscription tables.ProxySubscriptionTable
	if err := tx.First(&subscription, "id = ?", subscriptionID).Error; err != nil {
		return nil, err
	}

	groupIDs := []string{subscription.GroupID}
	var groups []*tables.ProxyGroupTable
	if err := tx.Where("subscription_id = ?", subscriptionID).Find(&groups).Error; err != nil {
		return nil, err
	}
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	return runtimeAffectedMappingIDsByGroups(ctx, tx, groupIDs)
}

func RuntimeStop() error {
	instances := replaceRuntimeInstances(RuntimeStatus{
		Running:   false,
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	})

	return closeRuntimeInstances(instances)
}

func syncRuntimeMappingDynamic(ctx context.Context, mapping *tables.PortMappingTable) (bool, RuntimeStatus, error) {
	if mapping == nil {
		return false, RuntimeStatusGet(), nil
	}
	existing := runtimeInstanceForMapping(mapping.ID)
	if existing == nil || existing.core == nil {
		return false, RuntimeStatusGet(), nil
	}
	nextInbound, err := buildMappingInbound(mapping)
	if err != nil {
		return false, RuntimeStatusGet(), err
	}
	nextInboundStatus := RuntimeInbound{
		MappingID: mapping.ID,
		Tag:       nextInbound.Tag,
		Listen:    mappingRuntimeListen(mapping),
		Outbound:  mappingOutboundTag(mapping.ID),
	}
	if existing.inboundKey != runtimeInboundKey(nextInboundStatus, mapping) {
		return false, RuntimeStatusGet(), nil
	}

	excludedNodes, failure := syncRuntimeInstanceMembership(ctx, mapping, existing)
	if failure != nil {
		return true, setRuntimeMappingFailure(mapping.ID, *failure, excludedNodes), nil
	}
	return true, setRuntimeMappingInstance(mapping.ID, existing, nextInboundStatus, excludedNodes), nil
}

func BuildSingBoxOptions(ctx context.Context, tx model.DBTx) (option.Options, []RuntimeInbound, error) {
	mappings, err := enabledRuntimeMappings(ctx, tx)
	if err != nil {
		return option.Options{}, nil, err
	}
	options, inbounds, _, err := buildSingBoxOptionsFromMappingsWithExcludedNodes(ctx, tx, mappings, nil)
	return options, inbounds, err
}

func enabledRuntimeMappings(ctx context.Context, tx model.DBTx) ([]*tables.PortMappingTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var mappings []*tables.PortMappingTable
	if err := tx.Where("enabled = ?", true).Order(mappingOrderClause()).Find(&mappings).Error; err != nil {
		return nil, err
	}
	return mappings, nil
}

func buildSingBoxOptionsFromMappingsWithExcludedNodes(
	ctx context.Context,
	tx model.DBTx,
	mappings []*tables.PortMappingTable,
	excludedNodeIDs map[string]struct{},
) (option.Options, []RuntimeInbound, map[string]*tables.ProxyNodeTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)
	outbounds := []option.Outbound{
		{
			Type:    constant.TypeDirect,
			Tag:     constant.TypeDirect,
			Options: &option.DirectOutboundOptions{},
		},
		{
			Type:    constant.TypeBlock,
			Tag:     constant.TypeBlock,
			Options: &option.StubOptions{},
		},
	}
	outboundTags := map[string]struct{}{
		constant.TypeDirect: {},
		constant.TypeBlock:  {},
	}
	blacklistedNodeIDs, err := nodeHealthBlacklistedIDs(ctx, tx)
	if err != nil {
		return option.Options{}, nil, nil, err
	}
	for nodeID := range excludedNodeIDs {
		blacklistedNodeIDs[nodeID] = struct{}{}
	}
	nodeCache := map[string]*tables.ProxyNodeTable{}
	outboundNodeCache := map[string]*tables.ProxyNodeTable{}
	groupCache := map[string]*tables.ProxyGroupTable{}
	nodeOutboundCache := map[string]string{}
	inbounds := make([]option.Inbound, 0, len(mappings))
	rules := make([]option.Rule, 0, len(mappings))
	statusInbounds := make([]RuntimeInbound, 0, len(mappings))

	for _, mapping := range mappings {
		nodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(mapping.NodeIDsJSON))
		if err != nil {
			return option.Options{}, nil, nil, err
		}

		memberTags := make([]string, 0, len(nodes))
		for _, node := range nodes {
			if _, blacklisted := blacklistedNodeIDs[node.ID]; blacklisted {
				continue
			}
			tag, nodeOutbounds, err := buildNodeRuntimeOutbounds(ctx, tx, node, outboundTags, nodeCache, outboundNodeCache, nodeOutboundCache, blacklistedNodeIDs)
			if err != nil {
				return option.Options{}, nil, outboundNodeCache, nodeBuildError{node: node, err: err}
			}
			memberTags = append(memberTags, tag)
			outbounds = append(outbounds, nodeOutbounds...)
		}

		groups, err := findGroupsByIDs(ctx, tx, decodeStringSlice(mapping.GroupIDsJSON))
		if err != nil {
			return option.Options{}, nil, nil, err
		}
		for _, proxyGroup := range groups {
			groupTag, groupOutbounds, err := buildProxyGroupOutbounds(
				ctx,
				tx,
				proxyGroup,
				outboundTags,
				nodeCache,
				outboundNodeCache,
				nodeOutboundCache,
				groupCache,
				blacklistedNodeIDs,
				map[string]bool{},
			)
			if err != nil {
				if buildErr, ok := asNodeBuildError(err); ok {
					return option.Options{}, nil, outboundNodeCache, buildErr
				}
				return option.Options{}, nil, outboundNodeCache, err
			}
			memberTags = append(memberTags, groupTag)
			outbounds = append(outbounds, groupOutbounds...)
		}

		routeTag, groupOutbound := buildMappingOutbound(mapping, memberTags)
		if groupOutbound != nil {
			if _, exists := outboundTags[routeTag]; !exists {
				outbounds = append(outbounds, *groupOutbound)
				outboundTags[routeTag] = struct{}{}
			}
		}

		inbound, err := buildMappingInbound(mapping)
		if err != nil {
			return option.Options{}, nil, nil, err
		}
		inbounds = append(inbounds, inbound)
		rules = append(rules, buildInboundRouteRule(inbound.Tag, routeTag))
		statusInbounds = append(statusInbounds, RuntimeInbound{
			MappingID: mapping.ID,
			Tag:       inbound.Tag,
			Listen:    mappingRuntimeListen(mapping),
			Outbound:  routeTag,
		})
	}

	options := option.Options{
		Log: &option.LogOptions{
			Level:        "warn",
			Output:       singBoxLogOutputPath(),
			Timestamp:    true,
			DisableColor: true,
		},
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Route: &option.RouteOptions{
			Rules: rules,
			Final: constant.TypeDirect,
		},
	}
	return options, statusInbounds, outboundNodeCache, nil
}

func buildDynamicRuntimePlanForMapping(
	ctx context.Context,
	tx model.DBTx,
	mapping *tables.PortMappingTable,
	excludedNodeIDs map[string]struct{},
) (*dynamicRuntimePlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)
	if mapping == nil {
		return nil, ErrMappingNotFound
	}

	inbound, err := buildMappingInbound(mapping)
	if err != nil {
		return nil, err
	}
	statusInbound := RuntimeInbound{
		MappingID: mapping.ID,
		Tag:       inbound.Tag,
		Listen:    mappingRuntimeListen(mapping),
		Outbound:  mappingOutboundTag(mapping.ID),
	}

	blacklistedNodeIDs, err := nodeHealthBlacklistedIDs(ctx, tx)
	if err != nil {
		return nil, err
	}
	for nodeID := range excludedNodeIDs {
		blacklistedNodeIDs[nodeID] = struct{}{}
	}

	builder := &dynamicPlanBuilder{
		ctx:                ctx,
		tx:                 tx,
		outbounds:          map[string]option.Outbound{},
		outboundNodes:      map[string]*tables.ProxyNodeTable{},
		groupPlans:         map[string]*dynamicGroupPlan{},
		blacklistedNodeIDs: blacklistedNodeIDs,
		excludedNodeIDs:    excludedNodeIDs,
	}

	members := make([]dynamicMemberPlan, 0)
	for _, builtin := range []string{} {
		_ = builtin
	}

	nodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(mapping.NodeIDsJSON))
	if err != nil {
		return nil, err
	}
	nodeMembers, err := builder.membersForNodes(nodes)
	if err != nil {
		return nil, err
	}
	if len(nodeMembers) == 0 {
		revived, err := builder.reviveIfAllCandidatesBlacklisted(nodeIDsFromNodes(nodes))
		if err != nil {
			return nil, err
		}
		if revived {
			nodeMembers, err = builder.membersForNodes(nodes)
			if err != nil {
				return nil, err
			}
		}
	}
	members = append(members, nodeMembers...)

	groups, err := findGroupsByIDs(ctx, tx, decodeStringSlice(mapping.GroupIDsJSON))
	if err != nil {
		return nil, err
	}
	for _, proxyGroup := range groups {
		member, err := builder.memberForGroup(proxyGroup, map[string]bool{})
		if err != nil {
			return nil, err
		}
		if member.tag != "" {
			members = append(members, member)
		}
	}

	members = uniqueDynamicMembers(members)
	if len(members) == 0 {
		members = []dynamicMemberPlan{builtinMember(constant.TypeBlock)}
	}
	mappingGroup := dynamicGroupPlan{
		tag:     mappingOutboundTag(mapping.ID),
		policy:  policyForMapping(mapping),
		members: members,
	}
	if normalizeStrategy(mapping.Strategy) == StrategyManual {
		mappingGroup.selected = selectedMappingMember(mapping, members)
	}
	builder.groupPlans[mappingGroup.tag] = &mappingGroup

	rules := []option.Rule{buildInboundRouteRule(inbound.Tag, mappingGroup.tag)}
	outbounds := singboxcore.BaseOutbounds()
	for _, outbound := range sortedOutbounds(builder.outbounds) {
		outbounds = append(outbounds, outbound)
	}

	return &dynamicRuntimePlan{
		options: option.Options{
			Log: &option.LogOptions{
				Level:        "warn",
				Output:       singBoxLogOutputPath(),
				Timestamp:    true,
				DisableColor: true,
			},
			Inbounds:  []option.Inbound{inbound},
			Outbounds: outbounds,
			Route: &option.RouteOptions{
				Rules: rules,
				Final: constant.TypeDirect,
			},
		},
		inbound:       statusInbound,
		inboundKey:    runtimeInboundKey(statusInbound, mapping),
		groups:        sortedGroupPlans(builder.groupPlans),
		outbounds:     builder.outbounds,
		outboundNodes: builder.outboundNodes,
	}, nil
}

func newRuntimeInstanceFromPlan(ctx context.Context, plan *dynamicRuntimePlan) (*runtimeInstance, []RuntimeExcludedNode, *RuntimeInboundFailure, *runtimeNodeFailure) {
	if plan == nil {
		failure := RuntimeInboundFailure{Error: "runtime plan was not created"}
		return nil, nil, &failure, nil
	}
	core, err := singboxcore.NewCore(singboxcore.Config{
		Context: ctx,
		Options: plan.options,
	})
	if err != nil {
		failure := runtimeFailureFromInbound(plan.inbound, err)
		return nil, nil, &failure, nil
	}
	for _, group := range plan.groups {
		if _, err := core.UpsertGroup(group.tag, group.policy); err != nil {
			_ = core.Close()
			failure := runtimeFailureFromInbound(plan.inbound, err)
			return nil, nil, &failure, nil
		}
	}
	instance := &runtimeInstance{core: core, inbound: plan.inbound, inboundKey: plan.inboundKey}
	excluded, err := applyDynamicRuntimePlan(ctx, plan, instance)
	if err != nil {
		_ = core.Close()
		if memberErr, ok := asDynamicMemberError(err); ok {
			if node := nodeFromDynamicMember(plan, memberErr.member); node != nil {
				return nil, excluded, nil, &runtimeNodeFailure{node: node, err: memberErr.err}
			}
		}
		failure := runtimeFailureFromInbound(plan.inbound, err)
		return nil, excluded, &failure, nil
	}
	if err := core.Start(); err != nil {
		_ = core.Close()
		failure := runtimeFailureFromInbound(plan.inbound, singboxcore.NormalizeStartError(err))
		return nil, nil, &failure, nil
	}
	return instance, nil, nil, nil
}

func syncRuntimeInstanceMembership(ctx context.Context, mapping *tables.PortMappingTable, instance *runtimeInstance) ([]RuntimeExcludedNode, *RuntimeInboundFailure) {
	excludedNodeIDs := map[string]struct{}{}
	excludedNodes := make([]RuntimeExcludedNode, 0)

	for {
		plan, err := buildDynamicRuntimePlanForMapping(ctx, nil, mapping, excludedNodeIDs)
		if err != nil {
			if buildErr, ok := asNodeBuildError(err); ok {
				retryNode := &runtimeNodeFailure{node: buildErr.node, err: buildErr.err}
				nextExcludedNodes, retry := excludeRuntimeNode(ctx, mapping, excludedNodeIDs, excludedNodes, nil, retryNode)
				excludedNodes = nextExcludedNodes
				if retry {
					continue
				}
				return excludedNodes, nil
			}
			failure := runtimeFailureFromMapping(mapping, err)
			return excludedNodes, &failure
		}

		nextExcludedNodes, failure, retryNode := applyDynamicRuntimePlanForMapping(ctx, plan, instance)
		excludedNodes = append(excludedNodes, nextExcludedNodes...)
		if retryNode == nil {
			return excludedNodes, failure
		}
		var retry bool
		excludedNodes, retry = excludeRuntimeNode(ctx, mapping, excludedNodeIDs, excludedNodes, plan.outboundNodes, retryNode)
		if !retry {
			if failure == nil {
				nextFailure := runtimeFailureFromMapping(mapping, retryNode.err)
				failure = &nextFailure
			}
			return excludedNodes, failure
		}
	}
}

func applyDynamicRuntimePlan(ctx context.Context, plan *dynamicRuntimePlan, instance *runtimeInstance) ([]RuntimeExcludedNode, error) {
	_ = ctx
	if plan == nil || instance == nil || instance.core == nil {
		return nil, errors.New("runtime instance was not created")
	}
	excludedNodes := make([]RuntimeExcludedNode, 0)
	for _, group := range plan.groups {
		if _, err := instance.core.UpsertGroup(group.tag, group.policy); err != nil {
			return excludedNodes, err
		}
		if err := syncDynamicGroupMembers(instance.core, group); err != nil {
			return excludedNodes, err
		}
	}
	instance.inbound = plan.inbound
	instance.inboundKey = plan.inboundKey
	return excludedNodes, nil
}

func applyDynamicRuntimePlanForMapping(
	ctx context.Context,
	plan *dynamicRuntimePlan,
	instance *runtimeInstance,
) ([]RuntimeExcludedNode, *RuntimeInboundFailure, *runtimeNodeFailure) {
	excludedNodes, err := applyDynamicRuntimePlan(ctx, plan, instance)
	if err == nil {
		return excludedNodes, nil, nil
	}
	memberErr, ok := asDynamicMemberError(err)
	if !ok {
		failure := runtimeFailureFromInbound(plan.inbound, err)
		return excludedNodes, &failure, nil
	}
	node := nodeFromDynamicMember(plan, memberErr.member)
	if node == nil {
		failure := runtimeFailureFromInbound(plan.inbound, err)
		return excludedNodes, &failure, nil
	}
	return excludedNodes, nil, &runtimeNodeFailure{node: node, err: memberErr.err}
}

func syncDynamicGroupMembers(core *singboxcore.Core, group dynamicGroupPlan) error {
	state := core.Snapshot()
	existing := map[string]singboxcore.NodeSnapshot{}
	for _, snapshot := range state.Groups {
		if snapshot.Tag != group.tag {
			continue
		}
		for _, node := range snapshot.Nodes {
			existing[node.ID] = node
		}
		break
	}

	next := map[string]dynamicMemberPlan{}
	for _, member := range group.members {
		if member.id == "" {
			member.id = member.tag
		}
		next[member.id] = member
		if member.builtin {
			continue
		}
		if _, ok := existing[member.id]; ok {
			continue
		}
		for _, outbound := range member.outbounds {
			if outbound.Tag == member.tag {
				continue
			}
			if err := core.CreateOutbound(outbound); err != nil {
				return dynamicMemberError{member: member, err: err}
			}
		}
		if err := core.AddNodeOutbound(group.tag, singboxcore.NodeConfig{
			ID:           member.id,
			Tag:          member.tag,
			Outbound:     member.outbound,
			OutboundTags: member.outboundTags(),
		}); err != nil {
			return dynamicMemberError{member: member, err: err}
		}
	}
	for nodeID, node := range existing {
		if _, ok := next[nodeID]; ok {
			continue
		}
		if node.Tag == constant.TypeDirect || node.Tag == constant.TypeBlock {
			_ = core.DisableNode(group.tag, nodeID)
			continue
		}
		if err := core.RemoveNode(group.tag, nodeID); err != nil && !errors.Is(err, singboxcore.ErrNodeNotFound) {
			return err
		}
	}
	for _, member := range group.members {
		if member.builtin {
			if _, ok := existing[member.id]; ok {
				continue
			}
			if err := addBuiltinMember(core, group.tag, member); err != nil {
				return dynamicMemberError{member: member, err: err}
			}
		}
	}
	if group.selected != "" {
		if err := core.SelectNode(group.tag, group.selected); err != nil && !errors.Is(err, singboxcore.ErrNoAvailableNode) {
			return err
		}
	}
	return core.GC()
}

func addBuiltinMember(core *singboxcore.Core, groupTag string, member dynamicMemberPlan) error {
	if member.id == "" {
		member.id = member.tag
	}
	outbound := option.Outbound{}
	switch member.tag {
	case constant.TypeDirect:
		outbound = option.Outbound{
			Type:    constant.TypeDirect,
			Tag:     constant.TypeDirect,
			Options: &option.DirectOutboundOptions{},
		}
	case constant.TypeBlock:
		outbound = option.Outbound{
			Type:    constant.TypeBlock,
			Tag:     constant.TypeBlock,
			Options: &option.StubOptions{},
		}
	default:
		return nil
	}
	return core.AddNodeOutbound(groupTag, singboxcore.NodeConfig{
		ID:       member.id,
		Tag:      member.tag,
		Outbound: outbound,
	})
}

type dynamicPlanBuilder struct {
	ctx                context.Context
	tx                 model.DBTx
	outbounds          map[string]option.Outbound
	outboundNodes      map[string]*tables.ProxyNodeTable
	groupPlans         map[string]*dynamicGroupPlan
	blacklistedNodeIDs map[string]struct{}
	excludedNodeIDs    map[string]struct{}
}

func (b *dynamicPlanBuilder) membersForNodes(nodes []*tables.ProxyNodeTable) ([]dynamicMemberPlan, error) {
	members := make([]dynamicMemberPlan, 0, len(nodes))
	for _, node := range nodes {
		member, err := b.memberForNode(node)
		if err != nil {
			return nil, nodeBuildError{node: node, err: err}
		}
		if member.tag != "" {
			members = append(members, member)
		}
	}
	return members, nil
}

func (b *dynamicPlanBuilder) reviveIfAllCandidatesBlacklisted(nodeIDs []string) (bool, error) {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return false, nil
	}
	for _, nodeID := range nodeIDs {
		if _, excluded := b.excludedNodeIDs[nodeID]; excluded {
			return false, nil
		}
		if _, blacklisted := b.blacklistedNodeIDs[nodeID]; !blacklisted {
			return false, nil
		}
	}
	reviveIDs, err := b.blacklistRevivalNodeIDs(nodeIDs, singboxcore.DefaultBlacklistRevivalLimit)
	if err != nil {
		return false, err
	}
	if len(reviveIDs) == 0 {
		return false, nil
	}
	if err := reviveNodeHealthIDs(b.ctx, b.tx, reviveIDs); err != nil {
		return false, err
	}
	for _, nodeID := range reviveIDs {
		delete(b.blacklistedNodeIDs, nodeID)
	}
	return true, nil
}

func (b *dynamicPlanBuilder) blacklistRevivalNodeIDs(nodeIDs []string, limit int) ([]string, error) {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = singboxcore.DefaultBlacklistRevivalLimit
	}
	if limit > len(nodeIDs) {
		limit = len(nodeIDs)
	}

	now := time.Now()
	var rows []*tables.ProxyNodeHealthTable
	if err := model.GetTx(b.tx).WithContext(b.ctx).
		Where("node_id IN ? AND blacklisted = ? AND (blacklisted_until IS NULL OR blacklisted_until > ?)", nodeIDs, true, now).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	healthByNodeID := make(map[string]*tables.ProxyNodeHealthTable, len(rows))
	for _, row := range rows {
		if row != nil {
			healthByNodeID[row.NodeID] = row
		}
	}

	candidates := make([]healthBlacklistRevivalCandidate, 0, len(nodeIDs))
	for order, nodeID := range nodeIDs {
		health := healthByNodeID[nodeID]
		if health == nil {
			continue
		}
		candidates = append(candidates, healthBlacklistRevivalCandidate{
			nodeID: nodeID,
			health: health,
			order:  order,
		})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return healthBlacklistRevivalCandidateLess(candidates[i], candidates[j])
	})

	if limit > len(candidates) {
		limit = len(candidates)
	}
	reviveIDs := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		reviveIDs = append(reviveIDs, candidates[i].nodeID)
	}
	return reviveIDs, nil
}

type healthBlacklistRevivalCandidate struct {
	nodeID string
	health *tables.ProxyNodeHealthTable
	order  int
}

func healthBlacklistRevivalCandidateLess(left, right healthBlacklistRevivalCandidate) bool {
	leftHealth := left.health
	rightHealth := right.health
	if leftHealth == nil || rightHealth == nil {
		return rightHealth == nil && leftHealth != nil
	}
	if leftHealth.ConsecutiveFailureCount != rightHealth.ConsecutiveFailureCount {
		return leftHealth.ConsecutiveFailureCount < rightHealth.ConsecutiveFailureCount
	}
	if cmp := compareHealthSuccessRatio(leftHealth, rightHealth); cmp != 0 {
		return cmp > 0
	}
	if !nullableTimeEqual(leftHealth.LastSuccessAt, rightHealth.LastSuccessAt) {
		return nullableTimeAfter(leftHealth.LastSuccessAt, rightHealth.LastSuccessAt)
	}
	if cmp := compareLatencyMs(leftHealth.LastLatencyMs, rightHealth.LastLatencyMs); cmp != 0 {
		return cmp < 0
	}
	if !nullableTimeEqual(leftHealth.LastCheckedAt, rightHealth.LastCheckedAt) {
		return nullableTimeBefore(leftHealth.LastCheckedAt, rightHealth.LastCheckedAt)
	}
	if !nullableTimeEqual(leftHealth.LastFailureAt, rightHealth.LastFailureAt) {
		return nullableTimeBefore(leftHealth.LastFailureAt, rightHealth.LastFailureAt)
	}
	if !nullableTimeEqual(leftHealth.BlacklistedUntil, rightHealth.BlacklistedUntil) {
		return nullableTimeBefore(leftHealth.BlacklistedUntil, rightHealth.BlacklistedUntil)
	}
	return left.order < right.order
}

func compareHealthSuccessRatio(left, right *tables.ProxyNodeHealthTable) int {
	leftTotal := int64(left.FailureCount) + left.SuccessCount
	rightTotal := int64(right.FailureCount) + right.SuccessCount
	if leftTotal == 0 || rightTotal == 0 {
		switch {
		case leftTotal > 0 && rightTotal == 0:
			return 1
		case leftTotal == 0 && rightTotal > 0:
			return -1
		default:
			return 0
		}
	}
	leftScore := left.SuccessCount * rightTotal
	rightScore := right.SuccessCount * leftTotal
	switch {
	case leftScore > rightScore:
		return 1
	case leftScore < rightScore:
		return -1
	default:
		return 0
	}
}

func compareLatencyMs(left, right int64) int {
	leftHasLatency := left > 0
	rightHasLatency := right > 0
	if leftHasLatency != rightHasLatency {
		if leftHasLatency {
			return -1
		}
		return 1
	}
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func nullableTimeEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func nullableTimeAfter(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left != nil
	}
	return left.After(*right)
}

func nullableTimeBefore(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left != nil && right == nil
	}
	return left.Before(*right)
}

func (b *dynamicPlanBuilder) memberForNode(node *tables.ProxyNodeTable) (dynamicMemberPlan, error) {
	if node == nil {
		return dynamicMemberPlan{}, nil
	}
	if _, blacklisted := b.blacklistedNodeIDs[node.ID]; blacklisted {
		return dynamicMemberPlan{}, nil
	}
	outboundTags := map[string]struct{}{
		constant.TypeDirect: {},
		constant.TypeBlock:  {},
	}
	for tag := range b.outbounds {
		outboundTags[tag] = struct{}{}
	}
	tag, outbounds, err := buildNodeRuntimeOutbounds(
		b.ctx,
		b.tx,
		node,
		outboundTags,
		map[string]*tables.ProxyNodeTable{},
		b.outboundNodes,
		map[string]string{},
		b.blacklistedNodeIDs,
	)
	if err != nil {
		return dynamicMemberPlan{}, err
	}
	for _, outbound := range outbounds {
		b.outbounds[outbound.Tag] = outbound
	}
	outbound, ok := b.outbounds[tag]
	if !ok {
		return dynamicMemberPlan{}, ErrNoAvailableNode
	}
	return dynamicMemberPlan{id: node.ID, tag: tag, outbound: outbound, outbounds: outbounds}, nil
}

func (b *dynamicPlanBuilder) memberForGroup(proxyGroup *tables.ProxyGroupTable, visiting map[string]bool) (dynamicMemberPlan, error) {
	if proxyGroup == nil {
		return dynamicMemberPlan{}, nil
	}
	tag := proxyGroupOutboundTag(proxyGroup.ID)
	if existing := b.groupPlans[tag]; existing != nil {
		return dynamicMemberPlan{
			id:  proxyGroup.ID,
			tag: existing.tag,
			outbound: option.Outbound{
				Type: singboxcore.DynamicOutboundType,
				Tag:  existing.tag,
			},
		}, nil
	}
	if visiting[proxyGroup.ID] {
		return dynamicMemberPlan{}, fmt.Errorf("%w: cyclic group %s", ErrInvalidGroup, proxyGroup.Name)
	}
	visiting[proxyGroup.ID] = true
	defer delete(visiting, proxyGroup.ID)

	members := make([]dynamicMemberPlan, 0)
	for _, builtin := range decodeStringSlice(proxyGroup.BuiltinTagsJSON) {
		switch builtin {
		case constantDirect:
			members = append(members, builtinMember(constant.TypeDirect))
		case constantReject, constantRejectDrop:
			members = append(members, builtinMember(constant.TypeBlock))
		}
	}

	nodes, err := findNodesByGroupOrIDs(b.ctx, b.tx, proxyGroup.ID, decodeStringSlice(proxyGroup.NodeIDsJSON))
	if err != nil {
		return dynamicMemberPlan{}, err
	}
	nodeMembers, err := b.membersForNodes(nodes)
	if err != nil {
		return dynamicMemberPlan{}, err
	}
	if len(nodeMembers) == 0 {
		revived, err := b.reviveIfAllCandidatesBlacklisted(nodeIDsFromNodes(nodes))
		if err != nil {
			return dynamicMemberPlan{}, err
		}
		if revived {
			nodeMembers, err = b.membersForNodes(nodes)
			if err != nil {
				return dynamicMemberPlan{}, err
			}
		}
	}
	members = append(members, nodeMembers...)

	childGroups, err := findGroupsByIDs(b.ctx, b.tx, decodeStringSlice(proxyGroup.GroupIDsJSON))
	if err != nil {
		return dynamicMemberPlan{}, err
	}
	for _, childGroup := range childGroups {
		member, err := b.memberForGroup(childGroup, visiting)
		if err != nil {
			return dynamicMemberPlan{}, err
		}
		if member.tag != "" {
			members = append(members, member)
		}
	}
	members = uniqueDynamicMembers(members)
	if len(members) == 0 {
		members = []dynamicMemberPlan{builtinMember(constant.TypeBlock)}
	}
	b.groupPlans[tag] = &dynamicGroupPlan{
		tag:      tag,
		policy:   policyForGroup(proxyGroup),
		members:  members,
		selected: members[0].id,
	}
	return dynamicMemberPlan{
		id:  proxyGroup.ID,
		tag: tag,
		outbound: option.Outbound{
			Type: singboxcore.DynamicOutboundType,
			Tag:  tag,
		},
	}, nil
}

func policyForMapping(mapping *tables.PortMappingTable) singboxcore.Policy {
	strategy := singboxcore.BalanceManual
	switch normalizeStrategy(mapping.Strategy) {
	case StrategyLoadBalance:
		strategy = singboxcore.BalanceRoundRobin
	case StrategyFailover:
		strategy = singboxcore.BalanceManual
	case StrategyLeastLatency:
		strategy = singboxcore.BalanceLeastLatency
	}
	healthConfig := normalizeHealthConfig(currentHealthConfig())
	return singboxcore.Policy{
		Strategy:                 strategy,
		FailureBlacklistTTL:      healthConfig.BlacklistDuration,
		RemoveTTL:                2 * time.Minute,
		ProbeURL:                 healthConfig.ProbeURL,
		ProbeInterval:            healthConfig.Interval,
		ProbeTimeout:             healthConfig.Timeout,
		ProbeTestTimeout:         minDuration(healthConfig.Timeout, singboxcore.DefaultLeastLatencyMaxLatency),
		ProbeConcurrency:         minPositive(healthConfig.MaxConcurrency, singboxcore.DefaultLeastLatencyProbeConcurrency),
		MaxLatency:               healthConfig.Timeout,
		SlowThreshold:            healthConfig.FailureThreshold,
		BlacklistRevivalLimit:    singboxcore.DefaultBlacklistRevivalLimit,
		FallbackStrategy:         singboxcore.BalanceRoundRobin,
		ProbeResultCallback:      recordRuntimeProbeResult,
		BlacklistRevivalCallback: reviveRuntimeBlacklistedNodes,
	}
}

func policyForGroup(group *tables.ProxyGroupTable) singboxcore.Policy {
	strategy := singboxcore.BalanceManual
	switch {
	case groupUsesLeastLatencyPolicy(group):
		strategy = singboxcore.BalanceLeastLatency
	case group != nil && normalizeGroupStrategy(group.Strategy) == GroupStrategyURLTest:
		strategy = singboxcore.BalanceRoundRobin
	}
	healthConfig := normalizeHealthConfig(currentHealthConfig())
	return singboxcore.Policy{
		Strategy:                 strategy,
		FailureBlacklistTTL:      healthConfig.BlacklistDuration,
		RemoveTTL:                2 * time.Minute,
		ProbeURL:                 healthConfig.ProbeURL,
		ProbeInterval:            healthConfig.Interval,
		ProbeTimeout:             healthConfig.Timeout,
		ProbeTestTimeout:         minDuration(healthConfig.Timeout, singboxcore.DefaultLeastLatencyMaxLatency),
		ProbeConcurrency:         minPositive(healthConfig.MaxConcurrency, singboxcore.DefaultLeastLatencyProbeConcurrency),
		MaxLatency:               healthConfig.Timeout,
		SlowThreshold:            healthConfig.FailureThreshold,
		BlacklistRevivalLimit:    singboxcore.DefaultBlacklistRevivalLimit,
		FallbackStrategy:         singboxcore.BalanceRoundRobin,
		ProbeResultCallback:      recordRuntimeProbeResult,
		BlacklistRevivalCallback: reviveRuntimeBlacklistedNodes,
	}
}

func groupUsesLeastLatencyPolicy(group *tables.ProxyGroupTable) bool {
	if group == nil {
		return false
	}
	strategy := normalizeGroupStrategy(group.Strategy)
	return strategy == GroupStrategyLeastLatency ||
		(group.Type == GroupTypeSubscription && strategy == GroupStrategyURLTest)
}

func minPositive(value int, max int) int {
	if value <= 0 {
		return max
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func minDuration(value time.Duration, max time.Duration) time.Duration {
	if value <= 0 {
		return max
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func selectedMappingMember(mapping *tables.PortMappingTable, members []dynamicMemberPlan) string {
	candidates := []string{}
	if mapping != nil {
		if mapping.ActiveGroupID != "" {
			candidates = append(candidates, mapping.ActiveGroupID, proxyGroupOutboundTag(mapping.ActiveGroupID))
		}
		if mapping.ActiveNodeID != "" {
			candidates = append(candidates, mapping.ActiveNodeID, nodeOutboundTag(mapping.ActiveNodeID))
		}
	}
	for _, candidate := range candidates {
		for _, member := range members {
			if member.id == candidate || member.tag == candidate {
				return member.id
			}
		}
	}
	if len(members) == 0 {
		return ""
	}
	return members[0].id
}

func builtinMember(tag string) dynamicMemberPlan {
	return dynamicMemberPlan{
		id:      tag,
		tag:     tag,
		builtin: true,
		outbound: option.Outbound{
			Type:    tag,
			Tag:     tag,
			Options: &option.StubOptions{},
		},
	}
}

func uniqueDynamicMembers(members []dynamicMemberPlan) []dynamicMemberPlan {
	seen := map[string]struct{}{}
	result := make([]dynamicMemberPlan, 0, len(members))
	for _, member := range members {
		key := strings.TrimSpace(member.id)
		if key == "" {
			key = strings.TrimSpace(member.tag)
		}
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, member)
	}
	return result
}

func sortedOutbounds(outbounds map[string]option.Outbound) []option.Outbound {
	tags := make([]string, 0, len(outbounds))
	for tag := range outbounds {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	result := make([]option.Outbound, 0, len(tags))
	for _, tag := range tags {
		result = append(result, outbounds[tag])
	}
	return result
}

func sortedGroupPlans(groups map[string]*dynamicGroupPlan) []dynamicGroupPlan {
	tags := make([]string, 0, len(groups))
	for tag := range groups {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	result := make([]dynamicGroupPlan, 0, len(tags))
	for _, tag := range tags {
		if group := groups[tag]; group != nil {
			result = append(result, *group)
		}
	}
	return result
}

func buildMappingInbound(mapping *tables.PortMappingTable) (option.Inbound, error) {
	listen, err := parseListenAddr(mapping.ListenAddress)
	if err != nil {
		return option.Inbound{}, err
	}

	listenOptions := option.ListenOptions{
		Listen:     listen,
		ListenPort: mapping.ListenPort,
	}
	users := inboundUsers(mapping.Username, mapping.Password)
	tag := mappingInboundTag(mapping.ID)

	switch normalizeOutboundProtocol(mapping.OutboundProtocol) {
	case OutboundProtocolSOCKS:
		return option.Inbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SocksInboundOptions{
				ListenOptions: listenOptions,
				Users:         users,
			},
		}, nil
	case OutboundProtocolHTTP:
		return option.Inbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPMixedInboundOptions{
				ListenOptions: listenOptions,
				Users:         users,
			},
		}, nil
	default:
		return option.Inbound{
			Type: constant.TypeMixed,
			Tag:  tag,
			Options: &option.HTTPMixedInboundOptions{
				ListenOptions: listenOptions,
				Users:         users,
			},
		}, nil
	}
}

func buildMappingOutbound(mapping *tables.PortMappingTable, nodeTags []string) (string, *option.Outbound) {
	if len(nodeTags) == 0 {
		return constant.TypeBlock, nil
	}
	if len(nodeTags) == 1 {
		return nodeTags[0], nil
	}

	activeTag := ""
	if normalizeStrategy(mapping.Strategy) == StrategyManual {
		if mapping.ActiveGroupID != "" {
			activeTag = proxyGroupOutboundTag(mapping.ActiveGroupID)
		}
		if activeTag == "" && mapping.ActiveNodeID != "" {
			activeTag = nodeOutboundTag(mapping.ActiveNodeID)
		}
	}
	if activeTag == "" || !containsString(nodeTags, activeTag) {
		activeTag = nodeTags[0]
	}

	groupTag := mappingOutboundTag(mapping.ID)
	return groupTag, &option.Outbound{
		Type: constant.TypeSelector,
		Tag:  groupTag,
		Options: &option.SelectorOutboundOptions{
			Outbounds: nodeTags,
			Default:   activeTag,
		},
	}
}

func buildProxyGroupOutbounds(
	ctx context.Context,
	tx model.DBTx,
	proxyGroup *tables.ProxyGroupTable,
	outboundTags map[string]struct{},
	nodeCache map[string]*tables.ProxyNodeTable,
	outboundNodeCache map[string]*tables.ProxyNodeTable,
	nodeOutboundCache map[string]string,
	groupCache map[string]*tables.ProxyGroupTable,
	blacklistedNodeIDs map[string]struct{},
	visiting map[string]bool,
) (string, []option.Outbound, error) {
	if proxyGroup == nil {
		return constant.TypeBlock, nil, nil
	}
	tag := proxyGroupOutboundTag(proxyGroup.ID)
	if _, exists := outboundTags[tag]; exists {
		return tag, nil, nil
	}
	if visiting[proxyGroup.ID] {
		return "", nil, fmt.Errorf("%w: cyclic group %s", ErrInvalidGroup, proxyGroup.Name)
	}
	visiting[proxyGroup.ID] = true
	defer delete(visiting, proxyGroup.ID)

	memberTags := make([]string, 0)
	outbounds := make([]option.Outbound, 0)

	for _, builtin := range decodeStringSlice(proxyGroup.BuiltinTagsJSON) {
		switch builtin {
		case constantDirect:
			memberTags = append(memberTags, constant.TypeDirect)
		case constantReject, constantRejectDrop:
			memberTags = append(memberTags, constant.TypeBlock)
		}
	}

	nodes, err := findNodesByGroupOrIDs(ctx, tx, proxyGroup.ID, decodeStringSlice(proxyGroup.NodeIDsJSON))
	if err != nil {
		return "", nil, err
	}
	for _, node := range nodes {
		if _, blacklisted := blacklistedNodeIDs[node.ID]; blacklisted {
			continue
		}
		nodeTag, nodeOutbounds, err := buildNodeRuntimeOutbounds(ctx, tx, node, outboundTags, nodeCache, outboundNodeCache, nodeOutboundCache, blacklistedNodeIDs)
		if err != nil {
			return "", nil, nodeBuildError{node: node, err: err}
		}
		memberTags = append(memberTags, nodeTag)
		outbounds = append(outbounds, nodeOutbounds...)
	}

	childGroups, err := findGroupsByIDs(ctx, tx, decodeStringSlice(proxyGroup.GroupIDsJSON))
	if err != nil {
		return "", nil, err
	}
	for _, childGroup := range childGroups {
		groupCache[childGroup.ID] = childGroup
		childTag, childOutbounds, err := buildProxyGroupOutbounds(
			ctx,
			tx,
			childGroup,
			outboundTags,
			nodeCache,
			outboundNodeCache,
			nodeOutboundCache,
			groupCache,
			blacklistedNodeIDs,
			visiting,
		)
		if err != nil {
			return "", nil, err
		}
		memberTags = append(memberTags, childTag)
		outbounds = append(outbounds, childOutbounds...)
	}

	memberTags = uniqueNonEmpty(memberTags)
	if len(memberTags) == 0 {
		memberTags = []string{constant.TypeBlock}
	}
	groupOutbound := buildProxyGroupOutbound(proxyGroup, tag, memberTags)
	outbounds = append(outbounds, groupOutbound)
	outboundTags[tag] = struct{}{}
	return tag, outbounds, nil
}

func buildNodeRuntimeOutbounds(
	ctx context.Context,
	tx model.DBTx,
	node *tables.ProxyNodeTable,
	outboundTags map[string]struct{},
	nodeCache map[string]*tables.ProxyNodeTable,
	outboundNodeCache map[string]*tables.ProxyNodeTable,
	nodeOutboundCache map[string]string,
	blacklistedNodeIDs map[string]struct{},
) (string, []option.Outbound, error) {
	if node == nil {
		return constant.TypeBlock, nil, nil
	}
	if tag, ok := nodeOutboundCache[node.ID]; ok {
		return tag, nil, nil
	}
	nodeCache[node.ID] = node

	if normalizeProtocol(node.Protocol) != ProtocolChain {
		tag := nodeOutboundTag(node.ID)
		nodeOutboundCache[node.ID] = tag
		if _, exists := outboundTags[tag]; exists {
			return tag, nil, nil
		}
		outbound, err := buildNodeOutbound(node, tag)
		if err != nil {
			return "", nil, err
		}
		outboundNodeCache[tag] = node
		outboundTags[tag] = struct{}{}
		return tag, []option.Outbound{outbound}, nil
	}

	chainNodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(node.ChainNodeIDsJSON))
	if err != nil {
		return "", nil, err
	}
	if len(chainNodes) < 2 {
		return "", nil, ErrInvalidChain
	}
	for _, chainNode := range chainNodes {
		if _, blacklisted := blacklistedNodeIDs[chainNode.ID]; blacklisted {
			return constant.TypeBlock, nil, nil
		}
	}

	outbounds := make([]option.Outbound, 0, len(chainNodes))
	detourTag := ""
	for index, chainNode := range chainNodes {
		if normalizeProtocol(chainNode.Protocol) == ProtocolChain {
			return "", nil, ErrInvalidChain
		}
		chainTag := nodeChainMemberOutboundTag(node.ID, index, chainNode.ID)
		if _, exists := outboundTags[chainTag]; exists {
			detourTag = chainTag
			continue
		}
		outbound, err := buildNodeOutbound(chainNode, chainTag)
		if err != nil {
			return "", nil, err
		}
		if detourTag != "" {
			if err := setOutboundDetour(&outbound, detourTag); err != nil {
				return "", nil, err
			}
		}
		outbounds = append(outbounds, outbound)
		outboundNodeCache[chainTag] = chainNode
		outboundTags[chainTag] = struct{}{}
		detourTag = chainTag
	}

	tag := nodeOutboundTag(node.ID)
	nodeOutboundCache[node.ID] = tag
	if _, exists := outboundTags[tag]; !exists {
		finalOutbound := outbounds[len(outbounds)-1]
		finalOutbound.Tag = tag
		if len(outbounds) > 1 {
			if err := setOutboundDetour(&finalOutbound, outbounds[len(outbounds)-2].Tag); err != nil {
				return "", nil, err
			}
		}
		outbounds = append(outbounds, finalOutbound)
		outboundNodeCache[tag] = node
		outboundTags[tag] = struct{}{}
	}
	return tag, outbounds, nil
}

func setOutboundDetour(outbound *option.Outbound, detour string) error {
	if outbound == nil || strings.TrimSpace(detour) == "" {
		return nil
	}
	wrapper, ok := outbound.Options.(option.DialerOptionsWrapper)
	if !ok {
		return ErrInvalidChain
	}
	dialOptions := wrapper.TakeDialerOptions()
	dialOptions.Detour = detour
	wrapper.ReplaceDialerOptions(dialOptions)
	return nil
}

func buildProxyGroupOutbound(proxyGroup *tables.ProxyGroupTable, tag string, memberTags []string) option.Outbound {
	defaultTag := memberTags[0]
	return option.Outbound{
		Type: constant.TypeSelector,
		Tag:  tag,
		Options: &option.SelectorOutboundOptions{
			Outbounds: memberTags,
			Default:   defaultTag,
		},
	}
}

func buildInboundRouteRule(inboundTag, outboundTag string) option.Rule {
	return option.Rule{
		Type: constant.RuleTypeDefault,
		DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{
				Inbound: badoption.Listable[string]{inboundTag},
			},
			RuleAction: option.RuleAction{
				Action: constant.RuleActionTypeRoute,
				RouteOptions: option.RouteActionOptions{
					Outbound: outboundTag,
				},
			},
		},
	}
}

func buildNodeOutbound(node *tables.ProxyNodeTable, tag string) (option.Outbound, error) {
	if strings.TrimSpace(node.RawURI) != "" {
		outbound, err := buildNodeOutboundFromURI(node.RawURI, tag)
		if err != nil {
			return option.Outbound{}, err
		}
		return outbound, nil
	}

	if node.Port == nil || *node.Port == 0 {
		return option.Outbound{}, ErrInvalidPort
	}
	serverOptions := option.ServerOptions{
		Server:     node.Server,
		ServerPort: *node.Port,
	}
	switch normalizeProtocol(node.Protocol) {
	case ProtocolVLESS:
		return option.Outbound{
			Type: constant.TypeVLESS,
			Tag:  tag,
			Options: &option.VLESSOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          node.Username,
			},
		}, nil
	case ProtocolVMess:
		return option.Outbound{
			Type: constant.TypeVMess,
			Tag:  tag,
			Options: &option.VMessOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          node.Username,
				Security:      "auto",
			},
		}, nil
	case ProtocolTrojan:
		return option.Outbound{
			Type: constant.TypeTrojan,
			Tag:  tag,
			Options: &option.TrojanOutboundOptions{
				ServerOptions: serverOptions,
				Password:      node.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: &option.OutboundTLSOptions{Enabled: true},
				},
			},
		}, nil
	case ProtocolSOCKS5:
		return option.Outbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SOCKSOutboundOptions{
				ServerOptions: serverOptions,
				Version:       "5",
				Username:      node.Username,
				Password:      node.Password,
			},
		}, nil
	case ProtocolHTTP:
		return option.Outbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPOutboundOptions{
				ServerOptions: serverOptions,
				Username:      node.Username,
				Password:      node.Password,
			},
		}, nil
	case ProtocolShadowsocks:
		if strings.TrimSpace(node.Username) == "" || strings.TrimSpace(node.Password) == "" {
			return option.Outbound{}, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
		}
		return option.Outbound{
			Type: constant.TypeShadowsocks,
			Tag:  tag,
			Options: &option.ShadowsocksOutboundOptions{
				ServerOptions: serverOptions,
				Method:        node.Username,
				Password:      node.Password,
			},
		}, nil
	case ProtocolTUIC:
		tlsOptions := defaultOutboundTLSOptions(serverOptions.Server)
		return option.Outbound{
			Type: constant.TypeTUIC,
			Tag:  tag,
			Options: &option.TUICOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          node.Username,
				Password:      node.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
			},
		}, nil
	case ProtocolSSH:
		return option.Outbound{
			Type: constant.TypeSSH,
			Tag:  tag,
			Options: &option.SSHOutboundOptions{
				ServerOptions: serverOptions,
				User:          node.Username,
				Password:      node.Password,
			},
		}, nil
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func defaultOutboundTLSOptions(serverName string) *option.OutboundTLSOptions {
	return &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: serverName,
	}
}

func parseListenAddr(value string) (*badoption.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return nil, ErrInvalidAddress
	}
	listen := badoption.Addr(addr)
	return &listen, nil
}

func inboundUsers(username, password string) []auth.User {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return nil
	}
	return []auth.User{{Username: username, Password: password}}
}

func nodeOutboundTag(id string) string {
	return "node-" + id
}

func nodeChainMemberOutboundTag(chainID string, index int, nodeID string) string {
	return fmt.Sprintf("node-chain-%s-%02d-%s", strings.TrimSpace(chainID), index, strings.TrimSpace(nodeID))
}

func mappingInboundTag(id string) string {
	return "mapping-in-" + id
}

func mappingOutboundTag(id string) string {
	return "mapping-out-" + id
}

func proxyGroupOutboundTag(id string) string {
	return "group-" + id
}

func runtimeFailureFromMapping(mapping *tables.PortMappingTable, err error) RuntimeInboundFailure {
	return RuntimeInboundFailure{
		MappingID: mapping.ID,
		Tag:       mappingInboundTag(mapping.ID),
		Listen:    mappingRuntimeListen(mapping),
		Error:     err.Error(),
	}
}

func runtimeFailureFromInbound(inbound RuntimeInbound, err error) RuntimeInboundFailure {
	return RuntimeInboundFailure{
		MappingID: inbound.MappingID,
		Tag:       inbound.Tag,
		Listen:    inbound.Listen,
		Error:     err.Error(),
	}
}

func runtimeAffectedMappingIDsByNodes(ctx context.Context, tx model.DBTx, nodeIDs []string) ([]string, error) {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return []string{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var groups []*tables.ProxyGroupTable
	if err := tx.Find(&groups).Error; err != nil {
		return nil, err
	}
	var allNodes []*tables.ProxyNodeTable
	if err := tx.Find(&allNodes).Error; err != nil {
		return nil, err
	}

	affectedNodeIDs := map[string]struct{}{}
	for _, nodeID := range nodeIDs {
		affectedNodeIDs[nodeID] = struct{}{}
	}
	changed := true
	for changed {
		changed = false
		currentNodeIDs := make([]string, 0, len(affectedNodeIDs))
		for nodeID := range affectedNodeIDs {
			currentNodeIDs = append(currentNodeIDs, nodeID)
		}
		for _, node := range allNodes {
			if normalizeProtocol(node.Protocol) != ProtocolChain {
				continue
			}
			if _, ok := affectedNodeIDs[node.ID]; ok {
				continue
			}
			if stringSlicesIntersect(decodeStringSlice(node.ChainNodeIDsJSON), currentNodeIDs) {
				affectedNodeIDs[node.ID] = struct{}{}
				changed = true
			}
		}
	}
	expandedNodeIDs := make([]string, 0, len(affectedNodeIDs))
	for nodeID := range affectedNodeIDs {
		expandedNodeIDs = append(expandedNodeIDs, nodeID)
	}

	affectedGroupIDs := map[string]struct{}{}
	for _, group := range groups {
		if stringSlicesIntersect(decodeStringSlice(group.NodeIDsJSON), expandedNodeIDs) {
			affectedGroupIDs[group.ID] = struct{}{}
		}
	}
	expandAffectedGroups(groups, affectedGroupIDs)

	groupIDs := make([]string, 0, len(affectedGroupIDs))
	for groupID := range affectedGroupIDs {
		groupIDs = append(groupIDs, groupID)
	}
	return runtimeAffectedMappingIDsByNodesAndGroups(ctx, tx, expandedNodeIDs, groupIDs)
}

func runtimeAffectedMappingIDsByGroups(ctx context.Context, tx model.DBTx, groupIDs []string) ([]string, error) {
	groupIDs = uniqueNonEmpty(groupIDs)
	if len(groupIDs) == 0 {
		return []string{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var groups []*tables.ProxyGroupTable
	if err := tx.Find(&groups).Error; err != nil {
		return nil, err
	}

	affectedGroupIDs := map[string]struct{}{}
	for _, groupID := range groupIDs {
		affectedGroupIDs[groupID] = struct{}{}
	}
	expandAffectedGroups(groups, affectedGroupIDs)

	expandedGroupIDs := make([]string, 0, len(affectedGroupIDs))
	for groupID := range affectedGroupIDs {
		expandedGroupIDs = append(expandedGroupIDs, groupID)
	}
	return runtimeAffectedMappingIDsByNodesAndGroups(ctx, tx, nil, expandedGroupIDs)
}

func runtimeAffectedMappingIDsByNodesAndGroups(ctx context.Context, tx model.DBTx, nodeIDs []string, groupIDs []string) ([]string, error) {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	groupIDs = uniqueNonEmpty(groupIDs)
	if len(nodeIDs) == 0 && len(groupIDs) == 0 {
		return []string{}, nil
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var mappings []*tables.PortMappingTable
	if err := tx.Find(&mappings).Error; err != nil {
		return nil, err
	}

	mappingIDs := make([]string, 0)
	for _, mapping := range mappings {
		if stringSlicesIntersect(decodeStringSlice(mapping.NodeIDsJSON), nodeIDs) ||
			stringSlicesIntersect(decodeStringSlice(mapping.GroupIDsJSON), groupIDs) {
			mappingIDs = append(mappingIDs, mapping.ID)
		}
	}
	return uniqueNonEmpty(mappingIDs), nil
}

func expandAffectedGroups(groups []*tables.ProxyGroupTable, affected map[string]struct{}) {
	changed := true
	for changed {
		changed = false
		for _, group := range groups {
			if _, ok := affected[group.ID]; ok {
				continue
			}
			for _, childGroupID := range decodeStringSlice(group.GroupIDsJSON) {
				if _, ok := affected[childGroupID]; ok {
					affected[group.ID] = struct{}{}
					changed = true
					break
				}
			}
		}
	}
}

func stringSlicesIntersect(first []string, second []string) bool {
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(second))
	for _, value := range second {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	for _, value := range first {
		if _, ok := seen[strings.TrimSpace(value)]; ok {
			return true
		}
	}
	return false
}

func mappingRuntimeListen(mapping *tables.PortMappingTable) string {
	if mapping == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", mapping.ListenAddress, mapping.ListenPort)
}

func runtimeInboundKey(inbound RuntimeInbound, mapping *tables.PortMappingTable) string {
	if mapping == nil {
		return inbound.Tag + "|" + inbound.Listen
	}
	return strings.Join([]string{
		inbound.Tag,
		mappingRuntimeListen(mapping),
		normalizeOutboundProtocol(mapping.OutboundProtocol),
		strings.TrimSpace(mapping.Username),
		strings.TrimSpace(mapping.Password),
	}, "|")
}

func nodeFromOutboundInitializeError(
	err error,
	outbounds []option.Outbound,
	outboundNodes map[string]*tables.ProxyNodeTable,
) *tables.ProxyNodeTable {
	if err == nil || len(outbounds) == 0 || len(outboundNodes) == 0 {
		return nil
	}
	index, ok := outboundInitializeErrorIndex(err.Error())
	if !ok || index < 0 || index >= len(outbounds) {
		return nil
	}
	return outboundNodes[outbounds[index].Tag]
}

func nodeFromDynamicMember(plan *dynamicRuntimePlan, member dynamicMemberPlan) *tables.ProxyNodeTable {
	if plan == nil || len(plan.outboundNodes) == 0 {
		return nil
	}
	if node := plan.outboundNodes[member.tag]; node != nil {
		return node
	}
	for _, tag := range member.outboundTags() {
		if node := plan.outboundNodes[tag]; node != nil {
			return node
		}
	}
	return nil
}

func outboundInitializeErrorIndex(message string) (int, bool) {
	const prefix = "initialize outbound["
	start := strings.Index(message, prefix)
	if start < 0 {
		return 0, false
	}
	start += len(prefix)
	end := strings.IndexByte(message[start:], ']')
	if end < 0 {
		return 0, false
	}
	index, err := strconv.Atoi(message[start : start+end])
	if err != nil {
		return 0, false
	}
	return index, true
}

func outboundTagForNode(outboundNodes map[string]*tables.ProxyNodeTable, node *tables.ProxyNodeTable) string {
	if node == nil {
		return ""
	}
	preferred := nodeOutboundTag(node.ID)
	if outboundNodes[preferred] != nil {
		return preferred
	}
	for tag, candidate := range outboundNodes {
		if candidate != nil && candidate.ID == node.ID {
			return tag
		}
	}
	return preferred
}

func runtimeExcludedNodeFromNode(
	mapping *tables.PortMappingTable,
	node *tables.ProxyNodeTable,
	tag string,
	err error,
) RuntimeExcludedNode {
	excluded := RuntimeExcludedNode{
		Tag:   tag,
		Error: errorString(err),
	}
	if mapping != nil {
		excluded.MappingID = mapping.ID
	}
	if node != nil {
		excluded.NodeID = node.ID
		excluded.NodeName = firstNonEmpty(node.Name, node.ID)
	}
	return excluded
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func createRuntimeMappingInstance(
	ctx context.Context,
	mapping *tables.PortMappingTable,
) (*runtimeInstance, RuntimeInbound, []RuntimeExcludedNode, *RuntimeInboundFailure) {
	excludedNodeIDs := map[string]struct{}{}
	excludedNodes := make([]RuntimeExcludedNode, 0)

	for {
		instance, inbound, outboundNodes, failure, retryNode := createRuntimeMappingInstanceOnce(ctx, mapping, excludedNodeIDs)
		if retryNode == nil {
			return instance, inbound, excludedNodes, failure
		}
		var retry bool
		excludedNodes, retry = excludeRuntimeNode(ctx, mapping, excludedNodeIDs, excludedNodes, outboundNodes, retryNode)
		if !retry {
			if failure == nil {
				nextFailure := runtimeFailureFromMapping(mapping, retryNode.err)
				failure = &nextFailure
			}
			return nil, RuntimeInbound{}, excludedNodes, failure
		}
	}
}

type runtimeNodeFailure struct {
	node *tables.ProxyNodeTable
	err  error
}

func excludeRuntimeNode(
	ctx context.Context,
	mapping *tables.PortMappingTable,
	excludedNodeIDs map[string]struct{},
	excludedNodes []RuntimeExcludedNode,
	outboundNodes map[string]*tables.ProxyNodeTable,
	retryNode *runtimeNodeFailure,
) ([]RuntimeExcludedNode, bool) {
	if retryNode == nil || retryNode.node == nil {
		return excludedNodes, false
	}
	if excludedNodeIDs == nil {
		excludedNodeIDs = map[string]struct{}{}
	}
	if _, exists := excludedNodeIDs[retryNode.node.ID]; exists {
		return excludedNodes, false
	}

	excluded := runtimeExcludedNodeFromNode(mapping, retryNode.node, outboundTagForNode(outboundNodes, retryNode.node), retryNode.err)
	excludedNodes = append(excludedNodes, excluded)
	excludedNodeIDs[retryNode.node.ID] = struct{}{}
	if _, err := blacklistRuntimeExcludedNode(ctx, retryNode.node, retryNode.err); err != nil {
		utils.Logger.Warn("自动排除节点写入健康状态失败",
			zap.String("mappingId", mapping.ID),
			zap.String("nodeId", retryNode.node.ID),
			zap.Error(err),
		)
	}
	utils.Logger.Warn("已自动排除运行时不可用节点",
		zap.String("mappingId", mapping.ID),
		zap.String("nodeId", retryNode.node.ID),
		zap.String("nodeName", retryNode.node.Name),
		zap.Error(retryNode.err),
	)
	return excludedNodes, true
}

func createRuntimeMappingInstanceOnce(
	ctx context.Context,
	mapping *tables.PortMappingTable,
	excludedNodeIDs map[string]struct{},
) (*runtimeInstance, RuntimeInbound, map[string]*tables.ProxyNodeTable, *RuntimeInboundFailure, *runtimeNodeFailure) {
	plan, err := buildDynamicRuntimePlanForMapping(ctx, nil, mapping, excludedNodeIDs)
	if err != nil {
		if buildErr, ok := asNodeBuildError(err); ok {
			return nil, RuntimeInbound{}, nil, nil, &runtimeNodeFailure{node: buildErr.node, err: buildErr.err}
		}
		failure := runtimeFailureFromMapping(mapping, err)
		return nil, RuntimeInbound{}, nil, &failure, nil
	}
	instance, excludedNodes, failure, retryNode := newRuntimeInstanceFromPlan(ctx, plan)
	if retryNode != nil {
		return nil, RuntimeInbound{}, plan.outboundNodes, nil, retryNode
	}
	if failure != nil {
		if node := nodeFromOutboundInitializeError(errors.New(failure.Error), plan.options.Outbounds, plan.outboundNodes); node != nil {
			return nil, RuntimeInbound{}, plan.outboundNodes, nil, &runtimeNodeFailure{node: node, err: errors.New(failure.Error)}
		}
		_ = excludedNodes
		return nil, RuntimeInbound{}, plan.outboundNodes, failure, nil
	}
	return instance, plan.inbound, plan.outboundNodes, nil, nil
}

func runtimeStatusFromResults(
	total int,
	inbounds []RuntimeInbound,
	failures []RuntimeInboundFailure,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	state := "stopped"
	errorMessage := ""
	switch {
	case total == 0:
		state = "stopped"
	case len(inbounds) > 0 && len(failures) == 0:
		state = "running"
	case len(inbounds) > 0:
		state = "degraded"
	default:
		state = "error"
		errorMessage = "all proxy runtime inbounds failed to start"
	}

	return RuntimeStatus{
		Running:  len(inbounds) > 0,
		State:    state,
		Error:    errorMessage,
		Inbounds: append([]RuntimeInbound(nil), inbounds...),
		Failures: append([]RuntimeInboundFailure(nil), failures...),
		ExcludedNodes: append(
			[]RuntimeExcludedNode(nil),
			excludedNodes...,
		),
		UpdatedAt: time.Now(),
	}
}

func runtimeStatusFromEntries(
	inbounds []RuntimeInbound,
	failures []RuntimeInboundFailure,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	return runtimeStatusFromResults(len(inbounds)+len(failures), inbounds, failures, excludedNodes)
}

func setRuntimeMappingInstance(
	mappingID string,
	instance *runtimeInstance,
	inbound RuntimeInbound,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	if singBoxRuntime.instances == nil {
		singBoxRuntime.instances = map[string]*runtimeInstance{}
	}
	singBoxRuntime.instances[mappingID] = instance
	inbounds := runtimeInboundsWithoutMapping(singBoxRuntime.status.Inbounds, mappingID)
	inbounds = append(inbounds, inbound)
	failures := runtimeFailuresWithoutMapping(singBoxRuntime.status.Failures, mappingID)
	allExcludedNodes := runtimeExcludedNodesWithoutMapping(singBoxRuntime.status.ExcludedNodes, mappingID)
	allExcludedNodes = append(allExcludedNodes, excludedNodes...)
	status := runtimeStatusFromEntries(inbounds, failures, allExcludedNodes)
	singBoxRuntime.status = normalizeRuntimeStatus(status)
	return singBoxRuntime.status
}

func setRuntimeMappingFailure(
	mappingID string,
	failure RuntimeInboundFailure,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	if singBoxRuntime.instances == nil {
		singBoxRuntime.instances = map[string]*runtimeInstance{}
	}
	delete(singBoxRuntime.instances, mappingID)
	inbounds := runtimeInboundsWithoutMapping(singBoxRuntime.status.Inbounds, mappingID)
	failures := runtimeFailuresWithoutMapping(singBoxRuntime.status.Failures, mappingID)
	failures = append(failures, failure)
	allExcludedNodes := runtimeExcludedNodesWithoutMapping(singBoxRuntime.status.ExcludedNodes, mappingID)
	allExcludedNodes = append(allExcludedNodes, excludedNodes...)
	status := runtimeStatusFromEntries(inbounds, failures, allExcludedNodes)
	singBoxRuntime.status = normalizeRuntimeStatus(status)
	return singBoxRuntime.status
}

func detachRuntimeMapping(mappingID string) *runtimeInstance {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	if singBoxRuntime.instances == nil {
		singBoxRuntime.instances = map[string]*runtimeInstance{}
	}
	instance := singBoxRuntime.instances[mappingID]
	delete(singBoxRuntime.instances, mappingID)
	inbounds := runtimeInboundsWithoutMapping(singBoxRuntime.status.Inbounds, mappingID)
	failures := runtimeFailuresWithoutMapping(singBoxRuntime.status.Failures, mappingID)
	excludedNodes := runtimeExcludedNodesWithoutMapping(singBoxRuntime.status.ExcludedNodes, mappingID)
	singBoxRuntime.status = normalizeRuntimeStatus(runtimeStatusFromEntries(inbounds, failures, excludedNodes))
	return instance
}

func runtimeInstanceForMapping(mappingID string) *runtimeInstance {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()
	if singBoxRuntime.instances == nil {
		return nil
	}
	return singBoxRuntime.instances[strings.TrimSpace(mappingID)]
}

func runtimeRoutesLocked() []RuntimeRoute {
	routes := make([]RuntimeRoute, 0)
	for mappingID, instance := range singBoxRuntime.instances {
		if instance == nil || instance.core == nil {
			continue
		}
		state := instance.core.Snapshot()
		groups := make(map[string]singboxcore.GroupSnapshot, len(state.Groups))
		for _, group := range state.Groups {
			groups[group.Tag] = group
		}
		for _, group := range state.Groups {
			routes = append(routes, runtimeRouteFromSnapshot(mappingID, group, groups))
		}
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].MappingID != routes[j].MappingID {
			return routes[i].MappingID < routes[j].MappingID
		}
		iRoot := routes[i].GroupTag == mappingOutboundTag(routes[i].MappingID)
		jRoot := routes[j].GroupTag == mappingOutboundTag(routes[j].MappingID)
		if iRoot != jRoot {
			return iRoot
		}
		return routes[i].GroupTag < routes[j].GroupTag
	})
	return routes
}

func runtimeRouteFromSnapshot(mappingID string, group singboxcore.GroupSnapshot, groups map[string]singboxcore.GroupSnapshot) RuntimeRoute {
	route := RuntimeRoute{
		MappingID:      mappingID,
		GroupTag:       group.Tag,
		Strategy:       string(group.Policy.Strategy),
		ProbeRunning:   group.ProbeRunning,
		RuntimeStarted: group.RuntimeStarted,
		LastProbeAt:    group.LastProbeAt,
		NextProbeAt:    group.NextProbeAt,
		Nodes:          make([]RuntimeRouteNode, 0, len(group.Nodes)),
	}
	for _, node := range group.Nodes {
		routeNode := runtimeRouteNodeFromSnapshot(group, node, groups)
		if routeNode.Selected {
			route.SelectedMemberID = routeNode.NodeID
			route.SelectedMemberTag = routeNode.NodeTag
		}
		route.Nodes = append(route.Nodes, routeNode)
	}
	if selected := resolveSelectedRuntimeRouteNode(group, groups, map[string]bool{}); selected != nil {
		route.SelectedNodeID = selected.NodeID
		route.SelectedNodeTag = selected.NodeTag
		route.SelectedNodeName = selected.NodeName
		route.SelectedNodeKind = selected.Kind
	}
	return route
}

func runtimeRouteNodeFromSnapshot(group singboxcore.GroupSnapshot, node singboxcore.NodeSnapshot, groups map[string]singboxcore.GroupSnapshot) RuntimeRouteNode {
	return RuntimeRouteNode{
		NodeID:            node.ID,
		NodeTag:           node.Tag,
		Kind:              runtimeRouteNodeKind(node, groups),
		Selected:          group.Selected == node.ID,
		Available:         runtimeSnapshotNodeAvailable(node),
		LatencyCandidate:  node.LatencyCandidate,
		LatencyFallback:   node.LatencyFallback,
		LatencySlowCount:  node.LatencySlowCount,
		LatencyMs:         node.LastLatencyMs,
		Error:             firstNonEmpty(node.LastProbeError, node.LastError),
		LastCheckedAt:     node.LastCheckedAt,
		LastSuccessAt:     node.LastSuccessAt,
		ProbeStartedAt:    node.ProbeStartedAt,
		ProbeRunning:      node.ProbeRunning,
		ProbeFailureCount: node.ProbeFailureCount,
	}
}

func runtimeRouteNodeKind(node singboxcore.NodeSnapshot, groups map[string]singboxcore.GroupSnapshot) string {
	if node.Tag == constant.TypeDirect || node.Tag == constant.TypeBlock || node.ID == constant.TypeDirect || node.ID == constant.TypeBlock {
		return "builtin"
	}
	if _, ok := groups[node.Tag]; ok {
		return "group"
	}
	return "node"
}

func runtimeSnapshotNodeAvailable(node singboxcore.NodeSnapshot) bool {
	if !node.Enabled || node.Tombstoned || node.Blacklisted || node.Health == singboxcore.HealthDead {
		return false
	}
	return firstNonEmpty(node.LastProbeError, node.LastError) == ""
}

func resolveSelectedRuntimeRouteNode(group singboxcore.GroupSnapshot, groups map[string]singboxcore.GroupSnapshot, visited map[string]bool) *RuntimeRouteNode {
	if visited[group.Tag] {
		return nil
	}
	visited[group.Tag] = true
	for _, node := range group.Nodes {
		if node.ID != group.Selected {
			continue
		}
		if child, ok := groups[node.Tag]; ok {
			if selected := resolveSelectedRuntimeRouteNode(child, groups, visited); selected != nil {
				return selected
			}
		}
		routeNode := runtimeRouteNodeFromSnapshot(group, node, groups)
		return &routeNode
	}
	return nil
}

func runtimeSelectedRouteNode(status RuntimeStatus, mappingID string) (RuntimeRouteNode, bool) {
	rootTag := mappingOutboundTag(mappingID)
	var fallback *RuntimeRoute
	for index := range status.Routes {
		route := &status.Routes[index]
		if route.MappingID != mappingID {
			continue
		}
		if route.GroupTag == rootTag {
			return selectedRouteNodeFromRoute(*route, status.Routes)
		}
		if fallback == nil {
			fallback = route
		}
	}
	if fallback == nil {
		return RuntimeRouteNode{}, false
	}
	return selectedRouteNodeFromRoute(*fallback, status.Routes)
}

func selectedRouteNodeFromRoute(route RuntimeRoute, routes []RuntimeRoute) (RuntimeRouteNode, bool) {
	if route.SelectedNodeID == "" && route.SelectedNodeTag == "" {
		return RuntimeRouteNode{}, false
	}
	for _, candidateRoute := range routes {
		if candidateRoute.MappingID != route.MappingID {
			continue
		}
		for _, node := range candidateRoute.Nodes {
			if node.NodeID == route.SelectedNodeID && node.NodeTag == route.SelectedNodeTag {
				return node, true
			}
		}
	}
	return RuntimeRouteNode{
		NodeID:   route.SelectedNodeID,
		NodeName: route.SelectedNodeName,
		NodeTag:  route.SelectedNodeTag,
		Kind:     route.SelectedNodeKind,
	}, true
}

func hydrateRuntimeRouteNames(ctx context.Context, routes []RuntimeRoute) []RuntimeRoute {
	if len(routes) == 0 {
		return []RuntimeRoute{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	nodeIDs := make([]string, 0)
	groupIDs := make([]string, 0)
	for _, route := range routes {
		for _, node := range route.Nodes {
			switch node.Kind {
			case "node":
				nodeIDs = append(nodeIDs, node.NodeID)
			case "group":
				groupIDs = append(groupIDs, node.NodeID)
			}
		}
		if route.SelectedNodeID != "" {
			switch route.SelectedNodeKind {
			case "node":
				nodeIDs = append(nodeIDs, route.SelectedNodeID)
			case "group":
				groupIDs = append(groupIDs, route.SelectedNodeID)
			}
		}
	}
	nodeNames := runtimeNodeNames(ctx, uniqueNonEmpty(nodeIDs))
	groupNames := runtimeGroupNames(ctx, uniqueNonEmpty(groupIDs))
	for routeIndex := range routes {
		for nodeIndex := range routes[routeIndex].Nodes {
			node := &routes[routeIndex].Nodes[nodeIndex]
			node.NodeName = runtimeRouteDisplayName(*node, nodeNames, groupNames)
		}
		if routes[routeIndex].SelectedNodeID == "" {
			continue
		}
		routes[routeIndex].SelectedNodeName = runtimeSelectedRouteDisplayName(routes[routeIndex], nodeNames, groupNames)
	}
	return routes
}

func runtimeNodeNames(ctx context.Context, ids []string) map[string]string {
	names := map[string]string{}
	if len(ids) == 0 {
		return names
	}
	var rows []*tables.ProxyNodeTable
	if err := model.GetTx(nil).WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		utils.Logger.Warn("读取运行时节点名称失败", zap.Error(err))
		return names
	}
	for _, row := range rows {
		if row != nil && row.ID != "" {
			names[row.ID] = row.Name
		}
	}
	return names
}

func runtimeGroupNames(ctx context.Context, ids []string) map[string]string {
	names := map[string]string{}
	if len(ids) == 0 {
		return names
	}
	var rows []*tables.ProxyGroupTable
	if err := model.GetTx(nil).WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		utils.Logger.Warn("读取运行时节点组名称失败", zap.Error(err))
		return names
	}
	for _, row := range rows {
		if row != nil && row.ID != "" {
			names[row.ID] = row.Name
		}
	}
	return names
}

func runtimeRouteDisplayName(node RuntimeRouteNode, nodeNames map[string]string, groupNames map[string]string) string {
	switch node.Kind {
	case "node":
		return firstNonEmpty(nodeNames[node.NodeID], node.NodeID)
	case "group":
		return firstNonEmpty(groupNames[node.NodeID], node.NodeID)
	case "builtin":
		return firstNonEmpty(node.NodeTag, node.NodeID)
	default:
		return node.NodeID
	}
}

func runtimeSelectedRouteDisplayName(route RuntimeRoute, nodeNames map[string]string, groupNames map[string]string) string {
	node := RuntimeRouteNode{
		NodeID:  route.SelectedNodeID,
		NodeTag: route.SelectedNodeTag,
		Kind:    route.SelectedNodeKind,
	}
	return runtimeRouteDisplayName(node, nodeNames, groupNames)
}

func runtimeInboundsWithoutMapping(inbounds []RuntimeInbound, mappingID string) []RuntimeInbound {
	result := make([]RuntimeInbound, 0, len(inbounds))
	for _, inbound := range inbounds {
		if inbound.MappingID != mappingID {
			result = append(result, inbound)
		}
	}
	return result
}

func runtimeFailuresWithoutMapping(failures []RuntimeInboundFailure, mappingID string) []RuntimeInboundFailure {
	result := make([]RuntimeInboundFailure, 0, len(failures))
	for _, failure := range failures {
		if failure.MappingID != mappingID {
			result = append(result, failure)
		}
	}
	return result
}

func runtimeExcludedNodesWithoutMapping(excludedNodes []RuntimeExcludedNode, mappingID string) []RuntimeExcludedNode {
	result := make([]RuntimeExcludedNode, 0, len(excludedNodes))
	for _, excludedNode := range excludedNodes {
		if excludedNode.MappingID != mappingID {
			result = append(result, excludedNode)
		}
	}
	return result
}

func replaceRuntimeInstances(status RuntimeStatus) map[string]*runtimeInstance {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	old := singBoxRuntime.instances
	singBoxRuntime.instances = map[string]*runtimeInstance{}
	singBoxRuntime.status = normalizeRuntimeStatus(status)
	return old
}

func setRuntimeInstances(instances map[string]*runtimeInstance, status RuntimeStatus) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	if instances == nil {
		instances = map[string]*runtimeInstance{}
	}
	status = normalizeRuntimeStatus(status)
	singBoxRuntime.instances = instances
	singBoxRuntime.status = status
	return status
}

func setRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	status = normalizeRuntimeStatus(status)
	singBoxRuntime.status = status
	return status
}

func normalizeRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	if status.Inbounds == nil {
		status.Inbounds = []RuntimeInbound{}
	}
	if status.Failures == nil {
		status.Failures = []RuntimeInboundFailure{}
	}
	if status.ExcludedNodes == nil {
		status.ExcludedNodes = []RuntimeExcludedNode{}
	}
	if status.Routes == nil {
		status.Routes = []RuntimeRoute{}
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	return status
}

func setRuntimeError(err error) RuntimeStatus {
	status := RuntimeStatus{
		Running:   false,
		State:     "error",
		Error:     err.Error(),
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	}
	return setRuntimeStatus(status)
}

func closeRuntimeInstances(instances map[string]*runtimeInstance) error {
	errs := make([]error, 0)
	for id, instance := range instances {
		if err := closeRuntimeInstance(id, instance); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeRuntimeInstance(id string, instance *runtimeInstance) error {
	if instance == nil {
		return nil
	}
	if err := instance.core.Close(); err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	return nil
}
