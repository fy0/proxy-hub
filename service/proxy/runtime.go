package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/byteformats"
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

type RuntimeStatus struct {
	Running       bool                    `json:"running"`
	State         string                  `json:"state"`
	Error         string                  `json:"error,omitempty"`
	Inbounds      []RuntimeInbound        `json:"inbounds"`
	Failures      []RuntimeInboundFailure `json:"failures"`
	ExcludedNodes []RuntimeExcludedNode   `json:"excludedNodes"`
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
	defer singBoxRuntime.mu.Unlock()

	status := singBoxRuntime.status
	status.Inbounds = append([]RuntimeInbound{}, singBoxRuntime.status.Inbounds...)
	status.Failures = append([]RuntimeInboundFailure{}, singBoxRuntime.status.Failures...)
	status.ExcludedNodes = append([]RuntimeExcludedNode{}, singBoxRuntime.status.ExcludedNodes...)
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

	return setRuntimeInstances(
		instances,
		runtimeStatusFromResults(len(mappings), inbounds, failures, excludedNodes),
	), nil
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
		return status, err
	}

	oldInstance := detachRuntimeMapping(mapping.ID)
	if closeErr := closeRuntimeInstance(mapping.ID, oldInstance); closeErr != nil {
		utils.Logger.Warn("关闭旧 sing-box 映射实例失败", zap.String("mappingId", mapping.ID), zap.Error(closeErr))
	}

	instance, inbound, excludedNodes, failure := createRuntimeMappingInstance(ctx, mapping)
	if failure != nil {
		return setRuntimeMappingFailure(mapping.ID, *failure, excludedNodes), nil
	}
	return setRuntimeMappingInstance(mapping.ID, instance, inbound, excludedNodes), nil
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
	return buildSingBoxOptionsFromMappings(ctx, tx, mappings)
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

func buildSingBoxOptionsFromMappings(
	ctx context.Context,
	tx model.DBTx,
	mappings []*tables.PortMappingTable,
) (option.Options, []RuntimeInbound, error) {
	options, inbounds, _, err := buildSingBoxOptionsFromMappingsWithExcludedNodes(ctx, tx, mappings, nil)
	return options, inbounds, err
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
	}

	members := make([]dynamicMemberPlan, 0)
	for _, builtin := range []string{} {
		_ = builtin
	}

	nodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(mapping.NodeIDsJSON))
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		member, err := builder.memberForNode(node)
		if err != nil {
			return nil, nodeBuildError{node: node, err: err}
		}
		if member.tag != "" {
			members = append(members, member)
		}
	}

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
		tag:      mappingOutboundTag(mapping.ID),
		policy:   policyForMapping(mapping),
		members:  members,
		selected: selectedMappingMember(mapping, members),
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

func newRuntimeInstanceFromPlan(ctx context.Context, plan *dynamicRuntimePlan) (*runtimeInstance, []RuntimeExcludedNode, *RuntimeInboundFailure) {
	if plan == nil {
		failure := RuntimeInboundFailure{Error: "runtime plan was not created"}
		return nil, nil, &failure
	}
	core, err := singboxcore.NewCore(singboxcore.Config{
		Context: ctx,
		Options: plan.options,
	})
	if err != nil {
		failure := runtimeFailureFromInbound(plan.inbound, err)
		return nil, nil, &failure
	}
	for _, group := range plan.groups {
		if _, err := core.UpsertGroup(group.tag, group.policy); err != nil {
			_ = core.Close()
			failure := runtimeFailureFromInbound(plan.inbound, err)
			return nil, nil, &failure
		}
	}
	instance := &runtimeInstance{core: core, inbound: plan.inbound, inboundKey: plan.inboundKey}
	if excluded, failure := applyDynamicRuntimePlan(ctx, plan, instance); failure != nil {
		_ = core.Close()
		return nil, excluded, failure
	}
	if err := core.Start(); err != nil {
		_ = core.Close()
		failure := runtimeFailureFromInbound(plan.inbound, singboxcore.NormalizeStartError(err))
		return nil, nil, &failure
	}
	return instance, nil, nil
}

func syncRuntimeInstanceMembership(ctx context.Context, mapping *tables.PortMappingTable, instance *runtimeInstance) ([]RuntimeExcludedNode, *RuntimeInboundFailure) {
	plan, err := buildDynamicRuntimePlanForMapping(ctx, nil, mapping, nil)
	if err != nil {
		if buildErr, ok := asNodeBuildError(err); ok {
			return []RuntimeExcludedNode{runtimeExcludedNodeFromNode(mapping, buildErr.node, outboundTagForNode(nil, buildErr.node), buildErr.err)}, nil
		}
		failure := runtimeFailureFromMapping(mapping, err)
		return nil, &failure
	}
	return applyDynamicRuntimePlan(ctx, plan, instance)
}

func applyDynamicRuntimePlan(ctx context.Context, plan *dynamicRuntimePlan, instance *runtimeInstance) ([]RuntimeExcludedNode, *RuntimeInboundFailure) {
	_ = ctx
	if plan == nil || instance == nil || instance.core == nil {
		failure := RuntimeInboundFailure{Error: "runtime instance was not created"}
		return nil, &failure
	}
	excludedNodes := make([]RuntimeExcludedNode, 0)
	for _, group := range plan.groups {
		if _, err := instance.core.UpsertGroup(group.tag, group.policy); err != nil {
			failure := runtimeFailureFromInbound(plan.inbound, err)
			return excludedNodes, &failure
		}
		if err := syncDynamicGroupMembers(instance.core, group); err != nil {
			failure := runtimeFailureFromInbound(plan.inbound, err)
			return excludedNodes, &failure
		}
	}
	instance.inbound = plan.inbound
	instance.inboundKey = plan.inboundKey
	return excludedNodes, nil
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
				return err
			}
		}
		if err := core.AddNodeOutbound(group.tag, singboxcore.NodeConfig{
			ID:           member.id,
			Tag:          member.tag,
			Outbound:     member.outbound,
			OutboundTags: member.outboundTags(),
		}); err != nil {
			return err
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
				return err
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
	for _, node := range nodes {
		member, err := b.memberForNode(node)
		if err != nil {
			return dynamicMemberPlan{}, nodeBuildError{node: node, err: err}
		}
		if member.tag != "" {
			members = append(members, member)
		}
	}

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
	}
	return singboxcore.Policy{
		Strategy:            strategy,
		FailureBlacklistTTL: normalizeHealthConfig(currentHealthConfig()).BlacklistDuration,
		RemoveTTL:           2 * time.Minute,
	}
}

func policyForGroup(group *tables.ProxyGroupTable) singboxcore.Policy {
	strategy := singboxcore.BalanceManual
	if group != nil && normalizeGroupStrategy(group.Strategy) == GroupStrategyURLTest {
		strategy = singboxcore.BalanceRoundRobin
	}
	return singboxcore.Policy{
		Strategy:            strategy,
		FailureBlacklistTTL: normalizeHealthConfig(currentHealthConfig()).BlacklistDuration,
		RemoveTTL:           2 * time.Minute,
	}
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
	if mapping.ActiveGroupID != "" {
		activeTag = proxyGroupOutboundTag(mapping.ActiveGroupID)
	}
	if activeTag == "" && mapping.ActiveNodeID != "" {
		activeTag = nodeOutboundTag(mapping.ActiveNodeID)
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

func buildNodeOutboundFromURI(rawURI string, tag string) (option.Outbound, error) {
	parsed, err := parseNodeURI(rawURI)
	if err != nil {
		return option.Outbound{}, err
	}
	serverOptions := option.ServerOptions{
		Server:     parsed.Server,
		ServerPort: *parsed.Port,
	}

	switch parsed.Protocol {
	case ProtocolVLESS:
		if requiresUTLS(parsed.Query) && !withUTLS {
			return option.Outbound{}, ErrUTLSRequired
		}
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeVLESS,
			Tag:  tag,
			Options: &option.VLESSOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          parsed.Username,
				Flow:          normalizeVLESSFlow(queryFirst(parsed.Query, "flow")),
				PacketEncoding: stringPtrOrNil(
					queryFirst(parsed.Query, "packetEncoding", "packet_encoding", "packet-encoding"),
				),
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
				Transport: transport,
			},
		}, nil
	case ProtocolVMess:
		if requiresUTLS(parsed.Query) && !withUTLS {
			return option.Outbound{}, ErrUTLSRequired
		}
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeVMess,
			Tag:  tag,
			Options: &option.VMessOutboundOptions{
				ServerOptions:  serverOptions,
				UUID:           parsed.Username,
				Security:       firstNonEmpty(parsed.VMessSecurity, "auto"),
				AlterId:        parsed.VMessAlterID,
				PacketEncoding: parsed.VMessPacketEncoding,
				Transport:      transport,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
			},
		}, nil
	case ProtocolTrojan:
		if requiresUTLS(parsed.Query) && !withUTLS {
			return option.Outbound{}, ErrUTLSRequired
		}
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeTrojan,
			Tag:  tag,
			Options: &option.TrojanOutboundOptions{
				ServerOptions: serverOptions,
				Password:      parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
				Transport: transport,
			},
		}, nil
	case ProtocolSOCKS5:
		return option.Outbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SOCKSOutboundOptions{
				ServerOptions: serverOptions,
				Version:       "5",
				Username:      parsed.Username,
				Password:      parsed.Password,
			},
		}, nil
	case ProtocolHTTP:
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPOutboundOptions{
				ServerOptions: serverOptions,
				Username:      parsed.Username,
				Password:      parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
			},
		}, nil
	case ProtocolShadowsocks:
		return buildShadowsocksOutbound(parsed, serverOptions, tag)
	case ProtocolHysteria:
		return buildHysteriaOutbound(parsed, serverOptions, tag)
	case ProtocolHysteria2:
		return buildHysteria2Outbound(parsed, serverOptions, tag)
	case ProtocolTUIC:
		return buildTUICOutbound(parsed, serverOptions, tag)
	case ProtocolSSH:
		return buildSSHOutbound(parsed, serverOptions, tag)
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func buildShadowsocksOutbound(parsed *parsedNodeURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	if strings.TrimSpace(parsed.Username) == "" || strings.TrimSpace(parsed.Password) == "" {
		return option.Outbound{}, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
	}
	options := &option.ShadowsocksOutboundOptions{
		ServerOptions: serverOptions,
		Method:        parsed.Username,
		Password:      parsed.Password,
		Plugin:        queryFirst(parsed.Query, "plugin"),
		PluginOptions: queryFirst(parsed.Query, "plugin_opts", "plugin-opts", "pluginOptions"),
		Network:       networkListFromQuery(parsed.Query),
	}
	return option.Outbound{Type: constant.TypeShadowsocks, Tag: tag, Options: options}, nil
}

func buildHysteriaOutbound(parsed *parsedNodeURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
	if err != nil {
		return option.Outbound{}, err
	}
	options := &option.HysteriaOutboundOptions{
		ServerOptions: serverOptions,
		ServerPorts:   listableStringFromQuery(parsed.Query, "server_ports", "server-ports", "ports"),
		HopInterval:   durationFromQuery(parsed.Query, "hop_interval", "hop-interval"),
		Obfs:          queryFirst(parsed.Query, "obfs"),
		AuthString:    firstNonEmpty(parsed.Password, queryFirst(parsed.Query, "auth_str", "auth-str", "password")),
		Network:       networkListFromQuery(parsed.Query),
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
	}
	if options.AuthString == "" {
		if auth := queryFirst(parsed.Query, "auth"); auth != "" {
			if decoded, err := decodeBase64Flexible(auth); err == nil {
				options.Auth = decoded
			} else {
				options.AuthString = auth
			}
		}
	}
	if up, err := networkBytesFromQuery(parsed.Query, "up"); err != nil {
		return option.Outbound{}, err
	} else {
		options.Up = up
	}
	if down, err := networkBytesFromQuery(parsed.Query, "down"); err != nil {
		return option.Outbound{}, err
	} else {
		options.Down = down
	}
	options.UpMbps = intFromQuery(parsed.Query, "up_mbps", "up-mbps", "upmbps")
	options.DownMbps = intFromQuery(parsed.Query, "down_mbps", "down-mbps", "downmbps")
	if options.Up == nil && options.UpMbps == 0 {
		return option.Outbound{}, fmt.Errorf("%w: missing hysteria upload bandwidth", ErrUnsupportedURI)
	}
	if options.Down == nil && options.DownMbps == 0 {
		return option.Outbound{}, fmt.Errorf("%w: missing hysteria download bandwidth", ErrUnsupportedURI)
	}
	options.ReceiveWindowConn = uint64FromQuery(parsed.Query, "recv_window_conn", "recv-window-conn")
	options.ReceiveWindow = uint64FromQuery(parsed.Query, "recv_window", "recv-window")
	options.DisableMTUDiscovery = queryBool(parsed.Query, "disable_mtu_discovery", "disable-mtu-discovery")
	return option.Outbound{Type: constant.TypeHysteria, Tag: tag, Options: options}, nil
}

func buildHysteria2Outbound(parsed *parsedNodeURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	if strings.TrimSpace(parsed.Password) == "" {
		return option.Outbound{}, fmt.Errorf("%w: missing hysteria2 password", ErrUnsupportedURI)
	}
	tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
	if err != nil {
		return option.Outbound{}, err
	}
	options := &option.Hysteria2OutboundOptions{
		ServerOptions: serverOptions,
		ServerPorts:   listableStringFromQuery(parsed.Query, "server_ports", "server-ports", "ports"),
		HopInterval:   durationFromQuery(parsed.Query, "hop_interval", "hop-interval"),
		UpMbps:        intFromQuery(parsed.Query, "up_mbps", "up-mbps", "upmbps"),
		DownMbps:      intFromQuery(parsed.Query, "down_mbps", "down-mbps", "downmbps"),
		Password:      parsed.Password,
		Network:       networkListFromQuery(parsed.Query),
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
		BrutalDebug: queryBool(parsed.Query, "brutal_debug", "brutal-debug"),
	}
	if obfsPassword := queryFirst(parsed.Query, "obfs-password", "obfs_password", "obfsPassword"); obfsPassword != "" {
		options.Obfs = &option.Hysteria2Obfs{
			Type:     firstNonEmpty(queryFirst(parsed.Query, "obfs", "obfs-type", "obfs_type"), "salamander"),
			Password: obfsPassword,
		}
	}
	return option.Outbound{Type: constant.TypeHysteria2, Tag: tag, Options: options}, nil
}

func buildTUICOutbound(parsed *parsedNodeURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	if strings.TrimSpace(parsed.Username) == "" {
		return option.Outbound{}, fmt.Errorf("%w: missing tuic uuid", ErrUnsupportedURI)
	}
	tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
	if err != nil {
		return option.Outbound{}, err
	}
	options := &option.TUICOutboundOptions{
		ServerOptions:     serverOptions,
		UUID:              parsed.Username,
		Password:          parsed.Password,
		CongestionControl: firstNonEmpty(queryFirst(parsed.Query, "congestion_control", "congestion-control"), "cubic"),
		UDPRelayMode:      queryFirst(parsed.Query, "udp_relay_mode", "udp-relay-mode"),
		UDPOverStream:     queryBool(parsed.Query, "udp_over_stream", "udp-over-stream"),
		ZeroRTTHandshake:  queryBool(parsed.Query, "zero_rtt_handshake", "zero-rtt-handshake"),
		Heartbeat:         durationFromQuery(parsed.Query, "heartbeat"),
		Network:           networkListFromQuery(parsed.Query),
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
	}
	return option.Outbound{Type: constant.TypeTUIC, Tag: tag, Options: options}, nil
}

func buildSSHOutbound(parsed *parsedNodeURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	options := &option.SSHOutboundOptions{
		ServerOptions:        serverOptions,
		User:                 parsed.Username,
		Password:             parsed.Password,
		PrivateKey:           listableStringFromQuery(parsed.Query, "private_key", "private-key"),
		PrivateKeyPath:       queryFirst(parsed.Query, "private_key_path", "private-key-path"),
		PrivateKeyPassphrase: queryFirst(parsed.Query, "private_key_passphrase", "private-key-passphrase"),
		HostKey:              listableStringFromQuery(parsed.Query, "host_key", "host-key"),
		HostKeyAlgorithms:    listableStringFromQuery(parsed.Query, "host_key_algorithms", "host-key-algorithms"),
		ClientVersion:        queryFirst(parsed.Query, "client_version", "client-version"),
	}
	return option.Outbound{Type: constant.TypeSSH, Tag: tag, Options: options}, nil
}

func buildTLSOptions(query url.Values, serverName string, defaultEnabled bool) (*option.OutboundTLSOptions, error) {
	security := securityMode(query)
	enabled := defaultEnabled || security == "tls" || security == "reality"
	if !enabled || security == "none" {
		return nil, nil
	}

	tlsOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: firstNonEmpty(queryFirst(query, "sni", "servername", "server_name"), serverName),
		Insecure:   queryBool(query, "allowInsecure", "allow_insecure", "insecure", "skip-cert-verify"),
	}
	if alpn := splitCommaList(queryFirst(query, "alpn")); len(alpn) > 0 {
		tlsOptions.ALPN = badoption.Listable[string](alpn)
	}
	fingerprint := queryFirst(query, "fp", "fingerprint")
	if security == "reality" {
		fingerprint = firstNonEmpty(fingerprint, "chrome")
	}
	if fingerprint != "" {
		tlsOptions.UTLS = &option.OutboundUTLSOptions{
			Enabled:     true,
			Fingerprint: fingerprint,
		}
	}
	if security == "reality" {
		publicKey := queryFirst(query, "pbk", "publicKey", "public_key")
		if publicKey == "" {
			return nil, fmt.Errorf("%w: missing reality public key", ErrUnsupportedURI)
		}
		tlsOptions.Reality = &option.OutboundRealityOptions{
			Enabled:   true,
			PublicKey: publicKey,
			ShortID:   queryFirst(query, "sid", "shortId", "short_id"),
		}
	}
	return tlsOptions, nil
}

func defaultOutboundTLSOptions(serverName string) *option.OutboundTLSOptions {
	return &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: serverName,
	}
}

func networkListFromQuery(query url.Values) option.NetworkList {
	network := strings.ToLower(strings.TrimSpace(queryFirst(query, "network")))
	switch network {
	case "tcp", "udp":
		return option.NetworkList(network)
	default:
		return ""
	}
}

func durationFromQuery(query url.Values, keys ...string) badoption.Duration {
	value := queryFirst(query, keys...)
	if value == "" {
		return 0
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return badoption.Duration(parsed)
}

func intFromQuery(query url.Values, keys ...string) int {
	value := queryFirst(query, keys...)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func uint64FromQuery(query url.Values, keys ...string) uint64 {
	value := queryFirst(query, keys...)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func listableStringFromQuery(query url.Values, keys ...string) badoption.Listable[string] {
	values := splitCommaList(queryFirst(query, keys...))
	if len(values) == 0 {
		return nil
	}
	return badoption.Listable[string](values)
}

func networkBytesFromQuery(query url.Values, keys ...string) (*byteformats.NetworkBytesCompat, error) {
	value := queryFirst(query, keys...)
	if value == "" {
		return nil, nil
	}
	data, err := strconv.Unquote(`"` + strings.ReplaceAll(value, `"`, `\"`) + `"`)
	if err != nil {
		data = value
	}
	var parsed byteformats.NetworkBytesCompat
	if err := parsed.UnmarshalJSON([]byte(strconv.Quote(data))); err != nil {
		return nil, fmt.Errorf("%w: invalid bandwidth %s", ErrUnsupportedURI, value)
	}
	return &parsed, nil
}

func requiresUTLS(query url.Values) bool {
	return securityMode(query) == "reality"
}

func normalizeVLESSFlow(flow string) string {
	flow = strings.TrimSpace(flow)
	const visionFlow = "xtls-rprx-vision"
	const udp443Suffix = "-udp443"

	suffix, found := strings.CutPrefix(flow, visionFlow)
	if found {
		for suffix != "" {
			if !strings.HasPrefix(suffix, udp443Suffix) {
				return flow
			}
			suffix = strings.TrimPrefix(suffix, udp443Suffix)
		}
		return visionFlow
	}
	return flow
}

func buildV2RayTransport(query url.Values) (*option.V2RayTransportOptions, error) {
	transportType, _ := transportTypeAndTag(query)
	switch transportType {
	case "":
		return nil, nil
	case constant.V2RayTransportTypeWebsocket:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeWebsocket}
		transport.WebsocketOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		if earlyData := queryFirst(query, "ed", "maxEarlyData", "max_early_data"); earlyData != "" {
			if parsed, err := strconv.ParseUint(earlyData, 10, 32); err == nil {
				transport.WebsocketOptions.MaxEarlyData = uint32(parsed)
			}
		}
		transport.WebsocketOptions.EarlyDataHeaderName = queryFirst(
			query,
			"eh",
			"earlyDataHeaderName",
			"early_data_header_name",
		)
		if host := queryFirst(query, "host"); host != "" {
			transport.WebsocketOptions.Headers = badoption.HTTPHeader{
				"Host": badoption.Listable[string]{host},
			}
		}
		return transport, nil
	case constant.V2RayTransportTypeHTTP:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeHTTP}
		transport.HTTPOptions.Path = queryFirst(query, "path")
		if host := splitCommaList(queryFirst(query, "host")); len(host) > 0 {
			transport.HTTPOptions.Host = badoption.Listable[string](host)
		}
		return transport, nil
	case constant.V2RayTransportTypeGRPC:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeGRPC}
		transport.GRPCOptions.ServiceName = firstNonEmpty(queryFirst(query, "serviceName", "service_name"), queryFirst(query, "path"))
		return transport, nil
	case constant.V2RayTransportTypeHTTPUpgrade:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeHTTPUpgrade}
		transport.HTTPUpgradeOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		transport.HTTPUpgradeOptions.Host = queryFirst(query, "host")
		return transport, nil
	case constant.V2RayTransportTypeQUIC:
		return &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeQUIC}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported transport %s", ErrUnsupportedURI, transportType)
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
		excluded.NodeName = node.Name
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
		if _, exists := excludedNodeIDs[retryNode.node.ID]; exists {
			if failure == nil {
				nextFailure := runtimeFailureFromMapping(mapping, retryNode.err)
				failure = &nextFailure
			}
			return nil, RuntimeInbound{}, excludedNodes, failure
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
	}
}

type runtimeNodeFailure struct {
	node *tables.ProxyNodeTable
	err  error
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
	instance, excludedNodes, failure := newRuntimeInstanceFromPlan(ctx, plan)
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
