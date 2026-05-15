package proxy

import (
	"context"
	"errors"
	"strings"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"

	"gorm.io/gorm"
)

func GroupCreate(ctx context.Context, tx model.DBTx, req GroupUpsertRequest) (*tables.ProxyGroupTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	normalized, err := normalizeGroupRequest(ctx, tx, "", req)
	if err != nil {
		return nil, err
	}
	group := &tables.ProxyGroupTable{
		Name:            normalized.Name,
		Type:            GroupTypeManual,
		Strategy:        normalized.Strategy,
		NodeIDsJSON:     encodeStringSlice(normalized.NodeIDs),
		GroupIDsJSON:    encodeStringSlice(normalized.GroupIDs),
		BuiltinTagsJSON: encodeStringSlice(nil),
		Remark:          normalized.Remark,
	}
	if err := tx.Create(group).Error; err != nil {
		return nil, err
	}
	if err := moveNodesToGroup(ctx, tx, group.ID, normalized.NodeIDs); err != nil {
		return nil, err
	}
	return group, nil
}

func GroupUpdate(ctx context.Context, tx model.DBTx, id string, req GroupUpsertRequest) (*tables.ProxyGroupTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var group tables.ProxyGroupTable
	if err := tx.First(&group, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}
	if group.Type == GroupTypeSubscription {
		return nil, ErrInvalidGroup
	}
	previousNodeIDs := decodeStringSlice(group.NodeIDsJSON)

	normalized, err := normalizeGroupRequest(ctx, tx, id, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Model(&group).Updates(map[string]any{
		"name":           normalized.Name,
		"strategy":       normalized.Strategy,
		"node_ids_json":  encodeStringSlice(normalized.NodeIDs),
		"group_ids_json": encodeStringSlice(normalized.GroupIDs),
		"remark":         normalized.Remark,
		"updated_at":     time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	removedNodeIDs := differenceStrings(previousNodeIDs, normalized.NodeIDs)
	if err := clearNodeGroup(ctx, tx, group.ID, removedNodeIDs); err != nil {
		return nil, err
	}
	if err := moveNodesToGroup(ctx, tx, group.ID, normalized.NodeIDs); err != nil {
		return nil, err
	}
	return GroupGet(ctx, tx, id)
}

func GroupGet(ctx context.Context, tx model.DBTx, id string) (*tables.ProxyGroupTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var group tables.ProxyGroupTable
	if err := tx.First(&group, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}
	return &group, nil
}

func GroupList(ctx context.Context, tx model.DBTx) ([]*tables.ProxyGroupTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var groups []*tables.ProxyGroupTable
	if err := tx.Order("type ASC, created_at DESC").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func GroupDelete(ctx context.Context, tx model.DBTx, id string) error {
	if tx != nil {
		return groupDeleteInTx(ctx, tx, id)
	}
	return model.Transaction(ctx, func(inner model.DBTx) error {
		return groupDeleteInTx(ctx, inner, id)
	})
}

func groupDeleteInTx(ctx context.Context, tx model.DBTx, id string) error {
	tx = model.GetTx(tx).WithContext(ctx)

	var group tables.ProxyGroupTable
	if err := tx.First(&group, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupNotFound
		}
		return err
	}
	if group.Type == GroupTypeSubscription {
		return ErrInvalidGroup
	}
	if err := cleanupGroupReferences(ctx, tx, []string{id}); err != nil {
		return err
	}
	if err := tx.Model(&tables.ProxyNodeTable{}).Where("group_id = ?", id).Updates(map[string]any{
		"group_id":   "",
		"updated_at": time.Now(),
	}).Error; err != nil {
		return err
	}
	if err := tx.Model(&tables.ProxySubscriptionTable{}).Where("group_id = ?", id).Updates(map[string]any{
		"group_id":   "",
		"updated_at": time.Now(),
	}).Error; err != nil {
		return err
	}
	return tx.Delete(&group).Error
}

func normalizeGroupRequest(ctx context.Context, tx model.DBTx, groupID string, req GroupUpsertRequest) (*GroupUpsertRequest, error) {
	normalized := req
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.Strategy = normalizeGroupStrategy(normalized.Strategy)
	normalized.Remark = strings.TrimSpace(normalized.Remark)
	if normalized.Name == "" {
		return nil, ErrInvalidGroup
	}

	nodes, err := findNodesByIDs(ctx, tx, normalized.NodeIDs)
	if err != nil {
		return nil, err
	}
	normalized.NodeIDs = make([]string, 0, len(nodes))
	for _, node := range nodes {
		normalized.NodeIDs = append(normalized.NodeIDs, node.ID)
	}

	groups, err := findGroupsByIDs(ctx, tx, normalized.GroupIDs)
	if err != nil {
		return nil, err
	}
	normalized.GroupIDs = make([]string, 0, len(groups))
	for _, group := range groups {
		if group.ID == groupID {
			continue
		}
		normalized.GroupIDs = append(normalized.GroupIDs, group.ID)
	}
	return &normalized, nil
}

func cleanupGroupReferences(ctx context.Context, tx model.DBTx, groupIDs []string) error {
	groupIDs = uniqueNonEmpty(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}

	var mappings []*tables.PortMappingTable
	if err := tx.WithContext(ctx).Find(&mappings).Error; err != nil {
		return err
	}
	for _, mapping := range mappings {
		nextGroupIDs := decodeStringSlice(mapping.GroupIDsJSON)
		for _, groupID := range groupIDs {
			nextGroupIDs = removeString(nextGroupIDs, groupID)
		}
		active := mapping.ActiveGroupID
		if containsString(groupIDs, active) {
			active = ""
			if len(nextGroupIDs) > 0 {
				active = nextGroupIDs[0]
			}
		}
		if err := tx.Model(mapping).Updates(map[string]any{
			"group_ids_json":  encodeStringSlice(nextGroupIDs),
			"active_group_id": active,
			"updated_at":      time.Now(),
		}).Error; err != nil {
			return err
		}
	}

	var groups []*tables.ProxyGroupTable
	if err := tx.WithContext(ctx).Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		nextGroupIDs := decodeStringSlice(group.GroupIDsJSON)
		for _, groupID := range groupIDs {
			nextGroupIDs = removeString(nextGroupIDs, groupID)
		}
		if err := tx.Model(group).Updates(map[string]any{
			"group_ids_json": encodeStringSlice(nextGroupIDs),
			"updated_at":     time.Now(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func moveNodesToGroup(ctx context.Context, tx model.DBTx, groupID string, nodeIDs []string) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}
	nodes, err := findNodesByIDs(ctx, tx, nodeIDs)
	if err != nil {
		return err
	}
	previousGroupNodeIDs := map[string][]string{}
	for _, node := range nodes {
		if node.GroupID == "" || node.GroupID == groupID {
			continue
		}
		previousGroupNodeIDs[node.GroupID] = append(previousGroupNodeIDs[node.GroupID], node.ID)
	}
	for previousGroupID, previousNodeIDs := range previousGroupNodeIDs {
		if err := removeNodesFromGroupMembership(ctx, tx, previousGroupID, previousNodeIDs); err != nil {
			return err
		}
	}
	if err := addNodesToGroupMembership(ctx, tx, groupID, nodeIDs); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&tables.ProxyNodeTable{}).
		Where("id IN ?", nodeIDs).
		Updates(map[string]any{
			"group_id":   groupID,
			"updated_at": time.Now(),
		}).Error
}

func clearNodeGroup(ctx context.Context, tx model.DBTx, groupID string, nodeIDs []string) error {
	groupID = strings.TrimSpace(groupID)
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if groupID == "" || len(nodeIDs) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&tables.ProxyNodeTable{}).
		Where("group_id = ? AND id IN ?", groupID, nodeIDs).
		Updates(map[string]any{
			"group_id":   "",
			"updated_at": time.Now(),
		}).Error
}

func addNodesToGroupMembership(ctx context.Context, tx model.DBTx, groupID string, nodeIDs []string) error {
	groupID = strings.TrimSpace(groupID)
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if groupID == "" || len(nodeIDs) == 0 {
		return nil
	}
	var group tables.ProxyGroupTable
	if err := tx.WithContext(ctx).First(&group, "id = ?", groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupNotFound
		}
		return err
	}
	nextNodeIDs := uniqueNonEmpty(append(decodeStringSlice(group.NodeIDsJSON), nodeIDs...))
	return tx.WithContext(ctx).Model(&group).Updates(map[string]any{
		"node_ids_json": encodeStringSlice(nextNodeIDs),
		"updated_at":    time.Now(),
	}).Error
}

func removeNodesFromGroupMembership(ctx context.Context, tx model.DBTx, groupID string, nodeIDs []string) error {
	groupID = strings.TrimSpace(groupID)
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if groupID == "" || len(nodeIDs) == 0 {
		return nil
	}
	var group tables.ProxyGroupTable
	if err := tx.WithContext(ctx).First(&group, "id = ?", groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	nextNodeIDs := decodeStringSlice(group.NodeIDsJSON)
	for _, nodeID := range nodeIDs {
		nextNodeIDs = removeString(nextNodeIDs, nodeID)
	}
	return tx.WithContext(ctx).Model(&group).Updates(map[string]any{
		"node_ids_json": encodeStringSlice(nextNodeIDs),
		"updated_at":    time.Now(),
	}).Error
}

func differenceStrings(values, excluded []string) []string {
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, value := range uniqueNonEmpty(excluded) {
		excludedSet[value] = struct{}{}
	}
	result := make([]string, 0)
	for _, value := range uniqueNonEmpty(values) {
		if _, ok := excludedSet[value]; ok {
			continue
		}
		result = append(result, value)
	}
	return result
}
