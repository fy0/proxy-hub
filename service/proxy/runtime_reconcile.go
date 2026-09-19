package proxy

import (
	"context"
	"net"
	"strconv"
	"strings"

	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/service/proxyuri"
)

func runtimeAffectedMappingIDsByNodes(ctx context.Context, tx model.DBTx, nodeIDs []string) ([]string, error) {
	nodeIDs = proxyuri.UniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return []string{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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
			if stringSlicesIntersect(chainNodeIDsFromMembers(chainMembersForNode(node)), currentNodeIDs) {
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
	for {
		groupChanged := false
		for _, group := range groups {
			if _, ok := affectedGroupIDs[group.ID]; ok {
				continue
			}
			if stringSlicesIntersect(decodeStringSlice(group.NodeIDsJSON), expandedNodeIDs) {
				affectedGroupIDs[group.ID] = struct{}{}
				groupChanged = true
			}
		}
		previousGroupCount := len(affectedGroupIDs)
		expandAffectedGroups(groups, affectedGroupIDs)
		if len(affectedGroupIDs) != previousGroupCount {
			groupChanged = true
		}

		currentGroupIDs := make([]string, 0, len(affectedGroupIDs))
		for groupID := range affectedGroupIDs {
			currentGroupIDs = append(currentGroupIDs, groupID)
		}
		nodeChanged := false
		for _, node := range allNodes {
			if node == nil || normalizeProtocol(node.Protocol) != ProtocolChain {
				continue
			}
			if _, ok := affectedNodeIDs[node.ID]; ok {
				continue
			}
			if stringSlicesIntersect(chainGroupIDsFromMembers(chainMembersForNode(node)), currentGroupIDs) {
				affectedNodeIDs[node.ID] = struct{}{}
				expandedNodeIDs = append(expandedNodeIDs, node.ID)
				nodeChanged = true
			}
		}
		if !groupChanged && !nodeChanged {
			break
		}
	}

	groupIDs := make([]string, 0, len(affectedGroupIDs))
	for groupID := range affectedGroupIDs {
		groupIDs = append(groupIDs, groupID)
	}
	return runtimeAffectedMappingIDsByNodesAndGroups(ctx, tx, expandedNodeIDs, groupIDs)
}

func runtimeAffectedMappingIDsByGroups(ctx context.Context, tx model.DBTx, groupIDs []string) ([]string, error) {
	groupIDs = proxyuri.UniqueNonEmpty(groupIDs)
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

	var allNodes []*tables.ProxyNodeTable
	if err := tx.Find(&allNodes).Error; err != nil {
		return nil, err
	}
	affectedNodeIDs := make([]string, 0)
	for _, node := range allNodes {
		if node == nil || normalizeProtocol(node.Protocol) != ProtocolChain {
			continue
		}
		if stringSlicesIntersect(chainGroupIDsFromMembers(chainMembersForNode(node)), expandedGroupIDs) {
			affectedNodeIDs = append(affectedNodeIDs, node.ID)
		}
	}
	if len(affectedNodeIDs) > 0 {
		nodeMappingIDs, err := runtimeAffectedMappingIDsByNodes(ctx, tx, affectedNodeIDs)
		if err != nil {
			return nil, err
		}
		groupMappingIDs, err := runtimeAffectedMappingIDsByNodesAndGroups(ctx, tx, nil, expandedGroupIDs)
		if err != nil {
			return nil, err
		}
		return proxyuri.UniqueNonEmpty(append(groupMappingIDs, nodeMappingIDs...)), nil
	}
	return runtimeAffectedMappingIDsByNodesAndGroups(ctx, tx, nil, expandedGroupIDs)
}

func runtimeAffectedMappingIDsByNodesAndGroups(ctx context.Context, tx model.DBTx, nodeIDs []string, groupIDs []string) ([]string, error) {
	nodeIDs = proxyuri.UniqueNonEmpty(nodeIDs)
	groupIDs = proxyuri.UniqueNonEmpty(groupIDs)
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
	return proxyuri.UniqueNonEmpty(mappingIDs), nil
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
	return net.JoinHostPort(mapping.ListenAddress, strconv.Itoa(int(mapping.ListenPort)))
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
