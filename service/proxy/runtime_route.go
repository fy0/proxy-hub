package proxy

import (
	"context"
	"sort"
	"strings"

	"github.com/sagernet/sing-box/constant"
	"go.uber.org/zap"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/service/proxyuri"
	"proxy-hub/utils"
)

func (m *runtimeManager) runtimeRoutesLocked() []RuntimeRoute {
	routes := make([]RuntimeRoute, 0)
	for mappingID, instance := range m.instances {
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

func runtimeRouteGroupID(tag string) string {
	tag = strings.TrimSpace(tag)
	if strings.HasPrefix(tag, "mapping-group-") {
		if index := strings.LastIndex(tag, "-group-"); index >= 0 {
			return tag[index+len("-group-"):]
		}
	}
	if strings.HasPrefix(tag, "node-chain-") {
		if index := strings.LastIndex(tag, "-group-"); index >= 0 {
			return tag[index+len("-group-"):]
		}
	}
	return strings.TrimPrefix(tag, "group-")
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
		Error:             proxyuri.FirstNonEmpty(node.LastProbeError, node.LastError),
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
	if _, ok := groups[node.Tag]; ok || strings.HasPrefix(node.Tag, "mapping-group-") {
		return "group"
	}
	return "node"
}

func runtimeSnapshotNodeAvailable(node singboxcore.NodeSnapshot) bool {
	if !node.Enabled || node.Tombstoned || node.Blacklisted || node.Health == singboxcore.HealthDead {
		return false
	}
	return proxyuri.FirstNonEmpty(node.LastProbeError, node.LastError) == ""
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
				if chainID, groupIndex, memberIndex, ok := parseNodeChainGroupTerminalNodeTag(node.NodeTag); ok {
					nodeIDs = append(nodeIDs, runtimeChainGroupMemberNodeID(chainID, groupIndex, memberIndex))
				}
			case "group":
				groupIDs = append(groupIDs, node.NodeID)
			}
		}
		if strings.HasPrefix(route.GroupTag, "mapping-group-") {
			groupIDs = append(groupIDs, runtimeRouteGroupID(route.GroupTag))
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
	nodeNames := runtimeNodeNames(ctx, proxyuri.UniqueNonEmpty(nodeIDs))
	groupNames := runtimeGroupNames(ctx, proxyuri.UniqueNonEmpty(groupIDs))
	for routeIndex := range routes {
		if strings.HasPrefix(routes[routeIndex].GroupTag, "mapping-group-") {
			groupID := runtimeRouteGroupID(routes[routeIndex].GroupTag)
			if routes[routeIndex].SelectedNodeID == "" {
				routes[routeIndex].SelectedNodeID = groupID
				routes[routeIndex].SelectedNodeTag = routes[routeIndex].GroupTag
				routes[routeIndex].SelectedNodeKind = "group"
			}
			routes[routeIndex].SelectedNodeName = proxyuri.FirstNonEmpty(groupNames[groupID], groupID)
		}
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
		if chainID, groupIndex, memberIndex, ok := parseNodeChainGroupTerminalNodeTag(node.NodeTag); ok {
			memberNodeID := runtimeChainGroupMemberNodeID(chainID, groupIndex, memberIndex)
			return proxyuri.FirstNonEmpty(nodeNames[memberNodeID], memberNodeID, nodeNames[node.NodeID], node.NodeID)
		}
		return proxyuri.FirstNonEmpty(nodeNames[node.NodeID], node.NodeID)
	case "group":
		return proxyuri.FirstNonEmpty(groupNames[node.NodeID], groupNames[runtimeRouteGroupID(node.NodeTag)], node.NodeID)
	case "builtin":
		return proxyuri.FirstNonEmpty(node.NodeTag, node.NodeID)
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
