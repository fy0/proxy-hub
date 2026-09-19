package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"go.uber.org/zap"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/service/proxyuri"
	"proxy-hub/utils"
	"slices"
)

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
	if !slices.Contains(tags, m.tag) {
		tags = append(tags, m.tag)
	}
	return proxyuri.UniqueNonEmpty(tags)
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
		ctx:                    ctx,
		tx:                     tx,
		outbounds:              map[string]option.Outbound{},
		outboundNodes:          map[string]*tables.ProxyNodeTable{},
		groupPlans:             map[string]*dynamicGroupPlan{},
		blacklistedNodeIDs:     blacklistedNodeIDs,
		excludedNodeIDs:        excludedNodeIDs,
		mappingID:              mapping.ID,
		groupStrategyOverrides: decodeGroupStrategyOverrides(mapping.GroupStrategyOverridesJSON),
	}

	members := make([]dynamicMemberPlan, 0)
	for _, builtin := range []string{} {
		_ = builtin
	}

	nodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(mapping.NodeIDsJSON))
	if err != nil {
		return nil, err
	}
	forceSelected := normalizeStrategy(mapping.Strategy) == StrategyManual
	nodeMembers, err := builder.membersForNodes(nodes, forceSelected)
	if err != nil {
		return nil, err
	}
	if len(nodeMembers) == 0 && !forceSelected {
		revived, err := builder.reviveIfAllCandidatesBlacklisted(nodeIDsFromNodes(nodes), mappingOutboundTag(mapping.ID))
		if err != nil {
			return nil, err
		}
		if revived {
			nodeMembers, err = builder.membersForNodes(nodes, false)
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
		member, err := builder.memberForMappingGroup(proxyGroup)
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
	if forceSelected {
		mappingGroup.selected = selectedMappingMember(mapping, members)
		// An explicitly selected but unbuildable member must not fall back to another route.
		if mappingGroup.selected == "" {
			mappingGroup.members = []dynamicMemberPlan{builtinMember(constant.TypeBlock)}
			mappingGroup.selected = constant.TypeBlock
		}
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
	if err := removeStaleDynamicGroups(instance.core, plan.groups); err != nil {
		return excludedNodes, err
	}
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

func removeStaleDynamicGroups(core *singboxcore.Core, groups []dynamicGroupPlan) error {
	if core == nil {
		return nil
	}
	desired := map[string]struct{}{}
	for _, group := range groups {
		if strings.TrimSpace(group.tag) != "" {
			desired[group.tag] = struct{}{}
		}
	}
	var joined error
	for _, snapshot := range core.Snapshot().Groups {
		if _, ok := desired[snapshot.Tag]; ok {
			continue
		}
		if err := core.RemoveGroup(snapshot.Tag); err != nil && !errors.Is(err, singboxcore.ErrGroupNotFound) {
			joined = errors.Join(joined, err)
		}
	}
	if joined != nil {
		return joined
	}
	return core.GC()
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
	ctx                    context.Context
	tx                     model.DBTx
	outbounds              map[string]option.Outbound
	outboundNodes          map[string]*tables.ProxyNodeTable
	groupPlans             map[string]*dynamicGroupPlan
	blacklistedNodeIDs     map[string]struct{}
	excludedNodeIDs        map[string]struct{}
	mappingID              string
	groupStrategyOverrides map[string]string
}

func (b *dynamicPlanBuilder) membersForNodes(nodes []*tables.ProxyNodeTable, ignoreBlacklist bool) ([]dynamicMemberPlan, error) {
	members := make([]dynamicMemberPlan, 0, len(nodes))
	for _, node := range nodes {
		member, err := b.memberForNode(node, ignoreBlacklist)
		if err != nil {
			return nil, nodeBuildError{node: node, err: err}
		}
		if member.tag != "" {
			members = append(members, member)
		}
	}
	return members, nil
}

func (b *dynamicPlanBuilder) reviveIfAllCandidatesBlacklisted(nodeIDs []string, groupTag string) (bool, error) {
	nodeIDs = proxyuri.UniqueNonEmpty(nodeIDs)
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
	utils.Logger.Info("运行时黑名单兜底复活节点",
		zap.String("mappingId", b.mappingID),
		zap.String("groupTag", strings.TrimSpace(groupTag)),
		zap.Strings("nodeIds", reviveIDs),
	)
	return true, nil
}

func (b *dynamicPlanBuilder) blacklistRevivalNodeIDs(nodeIDs []string, limit int) ([]string, error) {
	nodeIDs = proxyuri.UniqueNonEmpty(nodeIDs)
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
	healthByNodeID := NodeHealthMap(b.ctx, b.tx, nodeIDs)
	for nodeID, health := range healthByNodeID {
		if !isHealthBlacklisted(health, now) {
			delete(healthByNodeID, nodeID)
		}
	}

	candidates := make([]singboxcore.BlacklistRevivalInput, 0, len(nodeIDs))
	for order, nodeID := range nodeIDs {
		health := healthByNodeID[nodeID]
		if health == nil {
			continue
		}
		input := singboxcore.BlacklistRevivalInput{
			NodeID:       nodeID,
			Order:        order,
			FailureCount: health.ConsecutiveFailureCount,
			HasSuccess:   health.LastSuccessAt != nil,
			HasLatency:   health.LastLatencyMs > 0,
			LatencyMs:    health.LastLatencyMs,
		}
		if health.LastSuccessAt != nil {
			input.LastSuccessAt = *health.LastSuccessAt
		}
		if health.LastCheckedAt != nil {
			input.LastCheckedAt = *health.LastCheckedAt
		}
		if health.BlacklistedUntil != nil {
			input.BlacklistedUntil = *health.BlacklistedUntil
		}
		candidates = append(candidates, input)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	singboxcore.SortBlacklistRevivalInputs(candidates)

	if limit > len(candidates) {
		limit = len(candidates)
	}
	reviveIDs := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		reviveIDs = append(reviveIDs, candidates[i].NodeID)
	}
	return reviveIDs, nil
}

func (b *dynamicPlanBuilder) memberForNode(node *tables.ProxyNodeTable, ignoreBlacklist bool) (dynamicMemberPlan, error) {
	if node == nil {
		return dynamicMemberPlan{}, nil
	}
	if _, excluded := b.excludedNodeIDs[node.ID]; excluded {
		return dynamicMemberPlan{}, nil
	}
	if _, blacklisted := b.blacklistedNodeIDs[node.ID]; blacklisted && !ignoreBlacklist {
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
	)
	if err != nil {
		return dynamicMemberPlan{}, err
	}
	for _, outbound := range outbounds {
		b.outbounds[outbound.Tag] = outbound
	}
	b.cacheDynamicChainGroups(node)
	outbound, ok := b.outbounds[tag]
	if !ok {
		return dynamicMemberPlan{}, ErrNoAvailableNode
	}
	return dynamicMemberPlan{id: node.ID, tag: tag, outbound: outbound, outbounds: outbounds}, nil
}

func (b *dynamicPlanBuilder) memberForMappingGroup(proxyGroup *tables.ProxyGroupTable) (dynamicMemberPlan, error) {
	if proxyGroup == nil {
		return dynamicMemberPlan{}, nil
	}
	override := normalizeGroupStrategyOverride(b.groupStrategyOverrides[proxyGroup.ID])
	if override == "" || override == GroupStrategyOverrideInherit {
		return b.memberForGroup(proxyGroup, map[string]bool{})
	}
	return b.memberForGroupWithPolicy(
		proxyGroup,
		mappingProxyGroupOutboundTag(b.mappingID, proxyGroup.ID),
		policyForGroupStrategyOverride(proxyGroup, override),
		map[string]bool{},
	)
}

func (b *dynamicPlanBuilder) cacheDynamicChainGroups(node *tables.ProxyNodeTable) {
	if node == nil || normalizeProtocol(node.Protocol) != ProtocolChain {
		return
	}
	members := chainMembersForNode(node)
	for index, member := range members {
		if member.Type != ChainMemberTypeGroup {
			continue
		}
		group, err := findChainGroupByID(b.ctx, b.tx, member.ID)
		if err != nil || group == nil {
			continue
		}
		b.cacheDynamicChainGroup(node.ID, index, group, map[string]bool{})
	}
}

func (b *dynamicPlanBuilder) cacheDynamicChainGroup(
	chainID string,
	index int,
	group *tables.ProxyGroupTable,
	visiting map[string]bool,
) {
	if group == nil || visiting[group.ID] {
		return
	}
	visiting[group.ID] = true
	defer delete(visiting, group.ID)

	tag := nodeChainMemberGroupOutboundTag(chainID, index, group.ID)
	if _, exists := b.groupPlans[tag]; !exists {
		outbound := b.outbounds[tag]
		members := staticSelectorDynamicMembers(
			outboundTagsForSelector(outbound),
			b.outbounds,
			b.outboundNodes,
		)
		b.groupPlans[tag] = &dynamicGroupPlan{
			tag:      tag,
			policy:   policyForGroup(group),
			members:  members,
			selected: selectedDynamicMemberID(members, selectedSelectorOutboundTag(outbound)),
		}
		delete(b.outbounds, tag)
	}

	childGroups, err := findGroupsByIDs(b.ctx, b.tx, decodeStringSlice(group.GroupIDsJSON))
	if err != nil {
		return
	}
	for _, childGroup := range childGroups {
		b.cacheDynamicChainGroup(chainID, index, childGroup, visiting)
	}
}

func findChainGroupByID(ctx context.Context, tx model.DBTx, groupID string) (*tables.ProxyGroupTable, error) {
	groups, err := findGroupsByIDs(ctx, tx, []string{groupID})
	if err != nil {
		return nil, err
	}
	if len(groups) != 1 {
		return nil, ErrInvalidChain
	}
	return groups[0], nil
}

func staticSelectorDynamicMembers(
	tags []string,
	outbounds map[string]option.Outbound,
	outboundNodes map[string]*tables.ProxyNodeTable,
) []dynamicMemberPlan {
	tags = proxyuri.UniqueNonEmpty(tags)
	members := make([]dynamicMemberPlan, 0, len(tags))
	for _, tag := range tags {
		switch tag {
		case constant.TypeDirect, constant.TypeBlock:
			members = append(members, builtinMember(tag))
		default:
			outbound := outbounds[tag]
			if outbound.Tag == "" {
				outbound = option.Outbound{
					Type: singboxcore.DynamicOutboundType,
					Tag:  tag,
				}
			}
			id := tag
			terminalTag := false
			if chainID, groupIndex, memberIndex, ok := parseNodeChainGroupTerminalNodeTag(tag); ok {
				terminalTag = true
				id = proxyuri.FirstNonEmpty(runtimeChainGroupMemberNodeID(chainID, groupIndex, memberIndex), id)
			}
			if node := outboundNodes[tag]; node != nil && strings.TrimSpace(node.ID) != "" {
				if !terminalTag {
					id = node.ID
				}
			}
			members = append(members, dynamicMemberPlan{
				id:       id,
				tag:      tag,
				outbound: outbound,
				outbounds: collectOutboundDependencies(
					tag,
					outbounds,
					map[string]bool{},
				),
			})
		}
	}
	return uniqueDynamicMembers(members)
}

func selectedDynamicMemberID(members []dynamicMemberPlan, selectedTag string) string {
	selectedTag = strings.TrimSpace(selectedTag)
	for _, member := range members {
		if member.tag == selectedTag || member.id == selectedTag {
			return member.id
		}
	}
	if len(members) > 0 {
		return members[0].id
	}
	return ""
}

func collectOutboundDependencies(tag string, outbounds map[string]option.Outbound, visiting map[string]bool) []option.Outbound {
	tag = strings.TrimSpace(tag)
	if tag == "" || tag == constant.TypeDirect || tag == constant.TypeBlock || visiting[tag] {
		return nil
	}
	outbound := outbounds[tag]
	if outbound.Tag == "" {
		return nil
	}
	visiting[tag] = true
	defer delete(visiting, tag)

	result := make([]option.Outbound, 0)
	for _, childTag := range outboundTagsForSelector(outbound) {
		result = append(result, collectOutboundDependencies(childTag, outbounds, visiting)...)
	}
	result = append(result, outbound)
	return result
}

func outboundTagsForSelector(outbound option.Outbound) []string {
	switch options := outbound.Options.(type) {
	case *option.SelectorOutboundOptions:
		return options.Outbounds
	case option.SelectorOutboundOptions:
		return options.Outbounds
	default:
		return nil
	}
}

func selectedSelectorOutboundTag(outbound option.Outbound) string {
	switch options := outbound.Options.(type) {
	case *option.SelectorOutboundOptions:
		return proxyuri.FirstNonEmpty(options.Default, firstString(options.Outbounds))
	case option.SelectorOutboundOptions:
		return proxyuri.FirstNonEmpty(options.Default, firstString(options.Outbounds))
	default:
		return ""
	}
}

func firstString(values []string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (b *dynamicPlanBuilder) memberForGroup(proxyGroup *tables.ProxyGroupTable, visiting map[string]bool) (dynamicMemberPlan, error) {
	if proxyGroup == nil {
		return dynamicMemberPlan{}, nil
	}
	return b.memberForGroupWithPolicy(proxyGroup, proxyGroupOutboundTag(proxyGroup.ID), policyForGroup(proxyGroup), visiting)
}

func (b *dynamicPlanBuilder) memberForGroupWithPolicy(
	proxyGroup *tables.ProxyGroupTable,
	tag string,
	policy singboxcore.Policy,
	visiting map[string]bool,
) (dynamicMemberPlan, error) {
	if proxyGroup == nil {
		return dynamicMemberPlan{}, nil
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		tag = proxyGroupOutboundTag(proxyGroup.ID)
	}
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
	nodeMembers, err := b.membersForNodes(nodes, false)
	if err != nil {
		return dynamicMemberPlan{}, err
	}
	if len(nodeMembers) == 0 {
		revived, err := b.reviveIfAllCandidatesBlacklisted(nodeIDsFromNodes(nodes), tag)
		if err != nil {
			return dynamicMemberPlan{}, err
		}
		if revived {
			nodeMembers, err = b.membersForNodes(nodes, false)
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
		policy:   policy,
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
		ForceSelected:            normalizeStrategy(mapping.Strategy) == StrategyManual,
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
		TrafficFailureCallback:   recordRuntimeTrafficFailure,
	}
}

func policyForGroup(group *tables.ProxyGroupTable) singboxcore.Policy {
	strategy := singboxcore.BalanceManual
	switch {
	case groupUsesLeastLatencyPolicy(group):
		strategy = singboxcore.BalanceLeastLatency
	case groupUsesRoundRobinPolicy(group):
		strategy = singboxcore.BalanceRoundRobin
	case groupUsesRandomPolicy(group):
		strategy = singboxcore.BalanceRandom
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
		TrafficFailureCallback:   recordRuntimeTrafficFailure,
	}
}

func policyForGroupStrategyOverride(group *tables.ProxyGroupTable, override string) singboxcore.Policy {
	policy := policyForGroup(group)
	switch normalizeGroupStrategyOverride(override) {
	case GroupStrategyOverrideLoadBalance:
		policy.Strategy = singboxcore.BalanceRoundRobin
	case GroupStrategyOverrideLeastLatency:
		policy.Strategy = singboxcore.BalanceLeastLatency
	case GroupStrategyOverrideRandom:
		policy.Strategy = singboxcore.BalanceRandom
	}
	return policy
}

func groupUsesLeastLatencyPolicy(group *tables.ProxyGroupTable) bool {
	if group == nil {
		return false
	}
	return normalizeGroupStrategy(group.Strategy) == GroupStrategyLeastLatency
}

func groupUsesRoundRobinPolicy(group *tables.ProxyGroupTable) bool {
	if group == nil {
		return false
	}
	return normalizeGroupStrategy(group.Strategy) == GroupStrategyLoadBalance
}

func groupUsesRandomPolicy(group *tables.ProxyGroupTable) bool {
	if group == nil {
		return false
	}
	return normalizeGroupStrategy(group.Strategy) == GroupStrategyRandom
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
	if len(candidates) > 0 {
		return ""
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
