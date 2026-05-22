package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	subscriptionHTTPTimeout   = 30 * time.Second
	subscriptionMaxBytes      = 10 << 20
	subscriptionSyncBatchSize = 500
	constantDirect            = "DIRECT"
	constantReject            = "REJECT"
	constantRejectDrop        = "REJECT-DROP"
)

type subscriptionConfig struct {
	Proxies     []map[string]any `yaml:"proxies"`
	ProxyGroups []map[string]any `yaml:"proxy-groups"`
	Rules       []string         `yaml:"rules"`
}

type parsedSubscriptionNode struct {
	Name      string
	SourceKey string
	RawURI    string
}

type parsedSubscriptionGroup struct {
	Name        string
	SourceKey   string
	Strategy    string
	NodeNames   []string
	GroupNames  []string
	BuiltinTags []string
	IncludesAll bool
	Filter      string
}

type parsedClashConfig struct {
	Nodes        []parsedSubscriptionNode
	Groups       []parsedSubscriptionGroup
	Failures     []NodeImportFailure
	PreviewItems []NodeImportPreviewItem
}

type parsedSubscription struct {
	Nodes        []parsedSubscriptionNode
	Groups       []parsedSubscriptionGroup
	Failures     []NodeImportFailure
	PreviewItems []NodeImportPreviewItem
}

func SubscriptionCreate(ctx context.Context, tx model.DBTx, req SubscriptionUpsertRequest) (*tables.ProxySubscriptionTable, error) {
	if tx != nil {
		return subscriptionCreateInTx(ctx, tx, req)
	}
	var subscription *tables.ProxySubscriptionTable
	err := model.Transaction(ctx, func(inner model.DBTx) error {
		created, err := subscriptionCreateInTx(ctx, inner, req)
		if err != nil {
			return err
		}
		subscription = created
		return nil
	})
	return subscription, err
}

func subscriptionCreateInTx(ctx context.Context, tx model.DBTx, req SubscriptionUpsertRequest) (*tables.ProxySubscriptionTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	normalized, err := normalizeSubscriptionRequest(req)
	if err != nil {
		return nil, err
	}
	groupID, err := ensureSubscriptionRootGroup(ctx, tx, normalized.Name, normalized.GroupID)
	if err != nil {
		return nil, err
	}
	subscription := &tables.ProxySubscriptionTable{
		Name:    normalized.Name,
		URL:     normalized.URL,
		GroupID: groupID,
		Remark:  normalized.Remark,
	}
	if err := tx.Create(subscription).Error; err != nil {
		return nil, err
	}
	return subscription, nil
}

func SubscriptionUpdate(ctx context.Context, tx model.DBTx, id string, req SubscriptionUpsertRequest) (*tables.ProxySubscriptionTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var subscription tables.ProxySubscriptionTable
	if err := tx.First(&subscription, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	normalized, err := normalizeSubscriptionRequest(req)
	if err != nil {
		return nil, err
	}
	groupID, err := ensureSubscriptionRootGroup(ctx, tx, normalized.Name, normalized.GroupID)
	if err != nil {
		return nil, err
	}
	if err := tx.Model(&subscription).Updates(map[string]any{
		"name":       normalized.Name,
		"url":        normalized.URL,
		"group_id":   groupID,
		"remark":     normalized.Remark,
		"updated_at": time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return SubscriptionGet(ctx, tx, id)
}

func SubscriptionGet(ctx context.Context, tx model.DBTx, id string) (*tables.ProxySubscriptionTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var subscription tables.ProxySubscriptionTable
	if err := tx.First(&subscription, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return &subscription, nil
}

func SubscriptionList(ctx context.Context, tx model.DBTx) ([]*tables.ProxySubscriptionTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var subscriptions []*tables.ProxySubscriptionTable
	if err := tx.Order("created_at DESC").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func SubscriptionDelete(ctx context.Context, tx model.DBTx, id string) error {
	if tx != nil {
		return subscriptionDeleteInTx(ctx, tx, id)
	}
	return model.Transaction(ctx, func(inner model.DBTx) error {
		return subscriptionDeleteInTx(ctx, inner, id)
	})
}

func subscriptionDeleteInTx(ctx context.Context, tx model.DBTx, id string) error {
	tx = model.GetTx(tx).WithContext(ctx)

	var subscription tables.ProxySubscriptionTable
	if err := tx.First(&subscription, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSubscriptionNotFound
		}
		return err
	}

	var groups []*tables.ProxyGroupTable
	if err := tx.Where("subscription_id = ?", id).Find(&groups).Error; err != nil {
		return err
	}
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	if err := ensureGroupNotReferencedByChains(ctx, tx, groupIDs); err != nil {
		return err
	}
	if err := cleanupGroupReferences(ctx, tx, groupIDs); err != nil {
		return err
	}
	if len(groupIDs) > 0 {
		if err := tx.Where("id IN ?", groupIDs).Unscoped().Delete(&tables.ProxyGroupTable{}).Error; err != nil {
			return err
		}
	}

	var nodes []*tables.ProxyNodeTable
	if err := tx.Where("subscription_id = ?", id).Find(&nodes).Error; err != nil {
		return err
	}
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			nodeIDs = append(nodeIDs, node.ID)
		}
	}
	if err := deleteSubscriptionNodesBulk(ctx, tx, nodeIDs); err != nil {
		return err
	}
	return tx.Unscoped().Delete(&subscription).Error
}

func SubscriptionPreview(ctx context.Context, tx model.DBTx, req SubscriptionUpsertRequest) (*NodeImportResult, error) {
	normalized, err := normalizeSubscriptionRequest(req)
	if err != nil {
		return nil, err
	}
	raw, err := fetchSubscription(ctx, normalized.URL)
	if err != nil {
		return nil, err
	}
	return PreviewImportRaw(ctx, tx, raw)
}

func SubscriptionSync(ctx context.Context, tx model.DBTx, id string, req SubscriptionSyncRequest) (*NodeImportResult, error) {
	if tx != nil {
		return subscriptionSyncInTx(ctx, tx, id, req)
	}
	var result *NodeImportResult
	err := model.Transaction(ctx, func(inner model.DBTx) error {
		synced, err := subscriptionSyncInTx(ctx, inner, id, req)
		if err != nil {
			return err
		}
		result = synced
		return nil
	})
	if err != nil && !errors.Is(err, ErrSubscriptionNotFound) {
		_ = markSubscriptionSyncFailure(ctx, nil, id, err)
	}
	return result, err
}

func markSubscriptionSyncFailure(ctx context.Context, tx model.DBTx, id string, syncErr error) error {
	if syncErr == nil {
		return nil
	}
	now := time.Now()
	return model.GetTx(tx).WithContext(ctx).Model(&tables.ProxySubscriptionTable{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_synced_at":   &now,
			"last_sync_status": SubscriptionSyncStatusFailed,
			"last_sync_error":  syncErr.Error(),
			"updated_at":       now,
		}).Error
}

func subscriptionSyncInTx(ctx context.Context, tx model.DBTx, id string, req SubscriptionSyncRequest) (*NodeImportResult, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var subscription tables.ProxySubscriptionTable
	if err := tx.First(&subscription, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	if strings.TrimSpace(subscription.GroupID) == "" {
		groupID, err := ensureSubscriptionRootGroup(ctx, tx, subscription.Name, "")
		if err != nil {
			return nil, err
		}
		subscription.GroupID = groupID
		if err := tx.Model(&subscription).Updates(map[string]any{
			"group_id":   groupID,
			"updated_at": time.Now(),
		}).Error; err != nil {
			return nil, err
		}
	}

	raw := strings.TrimSpace(req.Raw)
	if raw == "" {
		fetched, err := fetchSubscription(ctx, subscription.URL)
		if err != nil {
			now := time.Now()
			_ = tx.Model(&subscription).Updates(map[string]any{
				"last_synced_at":   &now,
				"last_sync_status": SubscriptionSyncStatusFailed,
				"last_sync_error":  err.Error(),
				"updated_at":       now,
			}).Error
			return nil, err
		}
		raw = fetched
	}

	result, syncErr := syncSubscriptionRaw(ctx, tx, &subscription, raw)
	now := time.Now()
	status := SubscriptionSyncStatusSuccess
	errorText := ""
	if syncErr != nil {
		status = SubscriptionSyncStatusFailed
		errorText = syncErr.Error()
	}
	if err := tx.Model(&subscription).Updates(map[string]any{
		"last_synced_at":   &now,
		"last_sync_status": status,
		"last_sync_error":  errorText,
		"updated_at":       now,
	}).Error; err != nil && syncErr == nil {
		syncErr = err
	}
	return result, syncErr
}

func syncSubscriptionRaw(ctx context.Context, tx model.DBTx, subscription *tables.ProxySubscriptionTable, raw string) (*NodeImportResult, error) {
	parsed, err := parseSubscription(raw)
	if err != nil {
		return nil, err
	}

	result := &NodeImportResult{
		Total:    len(parsed.Nodes) + len(parsed.Failures) + skippedPreviewCount(parsed.PreviewItems),
		Failures: append([]NodeImportFailure(nil), parsed.Failures...),
		Skipped:  len(parsed.Failures) + skippedPreviewCount(parsed.PreviewItems),
	}
	existingNodes, err := nodesBySubscriptionSource(ctx, tx, subscription.ID)
	if err != nil {
		return nil, err
	}
	seenNodeKeys := map[string]struct{}{}
	nodeIDByName := map[string]string{}
	rootNodeIDs := make([]string, 0, len(parsed.Nodes))
	nodeRows := make([]*tables.ProxyNodeTable, 0, len(parsed.Nodes))
	nodeNamesByRow := make([]string, 0, len(parsed.Nodes))
	removedNodeIDsByGroup := map[string][]string{}

	for _, parsedNode := range parsed.Nodes {
		seenNodeKeys[parsedNode.SourceKey] = struct{}{}
		nodeReq, err := ParseNodeURI(parsedNode.RawURI)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: parsedNode.Name, Message: err.Error()})
			result.Skipped++
			continue
		}
		nodeReq.SubscriptionID = subscription.ID
		nodeReq.GroupID = subscription.GroupID
		nodeReq.SourceKey = parsedNode.SourceKey
		node, existed, err := subscriptionNodeRow(existingNodes[parsedNode.SourceKey], *nodeReq)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: parsedNode.Name, Message: err.Error()})
			result.Skipped++
			continue
		}
		if existed {
			result.Updated++
		} else {
			result.Imported++
		}
		if existed && existingNodes[parsedNode.SourceKey] != nil {
			previousGroupID := strings.TrimSpace(existingNodes[parsedNode.SourceKey].GroupID)
			if previousGroupID != "" && previousGroupID != subscription.GroupID {
				removedNodeIDsByGroup[previousGroupID] = append(removedNodeIDsByGroup[previousGroupID], node.ID)
			}
		}
		nodeRows = append(nodeRows, node)
		nodeNamesByRow = append(nodeNamesByRow, parsedNode.Name)
	}
	if err := upsertSubscriptionNodeRows(ctx, tx, nodeRows); err != nil {
		return result, err
	}
	for index, node := range nodeRows {
		if node == nil {
			continue
		}
		rootNodeIDs = append(rootNodeIDs, node.ID)
		nodeIDByName[node.Name] = node.ID
		if index < len(nodeNamesByRow) {
			nodeIDByName[nodeNamesByRow[index]] = node.ID
		}
	}
	for groupID, nodeIDs := range removedNodeIDsByGroup {
		if err := removeNodesFromGroupMembership(ctx, tx, groupID, nodeIDs); err != nil {
			return result, err
		}
	}

	deletedNodeIDs := make([]string, 0)
	for sourceKey, node := range existingNodes {
		if _, ok := seenNodeKeys[sourceKey]; ok {
			continue
		}
		deletedNodeIDs = append(deletedNodeIDs, node.ID)
	}
	if err := deleteSubscriptionNodesBulk(ctx, tx, deletedNodeIDs); err != nil {
		return result, err
	}
	result.Deleted += len(deletedNodeIDs)

	existingGroups, err := groupsBySubscriptionSource(ctx, tx, subscription.ID)
	if err != nil {
		return nil, err
	}
	seenGroupKeys := map[string]struct{}{}
	groupIDByName := map[string]string{}
	rootGroupIDs := make([]string, 0, len(parsed.Groups))
	for _, parsedGroup := range parsed.Groups {
		seenGroupKeys[parsedGroup.SourceKey] = struct{}{}
		group, err := ensureSubscriptionGroupShell(ctx, tx, subscription.ID, existingGroups[parsedGroup.SourceKey], parsedGroup)
		if err != nil {
			return result, err
		}
		existingGroups[parsedGroup.SourceKey] = group
		groupIDByName[group.Name] = group.ID
		rootGroupIDs = append(rootGroupIDs, group.ID)
	}

	for _, parsedGroup := range parsed.Groups {
		group := existingGroups[parsedGroup.SourceKey]
		nodeIDs, groupIDs := subscriptionGroupMembers(parsedGroup, nodeIDByName, groupIDByName)
		if err := tx.Model(group).Updates(map[string]any{
			"strategy":          parsedGroup.Strategy,
			"node_ids_json":     encodeStringSlice(nodeIDs),
			"group_ids_json":    encodeStringSlice(removeString(groupIDs, group.ID)),
			"builtin_tags_json": encodeStringSlice(parsedGroup.BuiltinTags),
			"includes_all":      parsedGroup.IncludesAll,
			"filter":            parsedGroup.Filter,
			"updated_at":        time.Now(),
		}).Error; err != nil {
			return result, err
		}
	}

	deletedGroupIDs := make([]string, 0)
	for sourceKey, group := range existingGroups {
		if _, ok := seenGroupKeys[sourceKey]; ok {
			continue
		}
		deletedGroupIDs = append(deletedGroupIDs, group.ID)
	}
	if err := ensureGroupNotReferencedByChains(ctx, tx, deletedGroupIDs); err != nil {
		return result, err
	}
	if err := cleanupGroupReferences(ctx, tx, deletedGroupIDs); err != nil {
		return result, err
	}
	if len(deletedGroupIDs) > 0 {
		if err := tx.Where("id IN ?", deletedGroupIDs).Unscoped().Delete(&tables.ProxyGroupTable{}).Error; err != nil {
			return result, err
		}
		result.Deleted += len(deletedGroupIDs)
	}
	if err := updateRootGroupReferences(ctx, tx, subscription.GroupID, rootNodeIDs, rootGroupIDs); err != nil {
		return result, err
	}
	result.Failed = len(result.Failures)
	return result, nil
}

func subscriptionNodeRow(existing *tables.ProxyNodeTable, req NodeUpsertRequest) (*tables.ProxyNodeTable, bool, error) {
	normalized, err := normalizeNodeRequest(req)
	if err != nil {
		return nil, false, err
	}
	node := &tables.ProxyNodeTable{
		Name:             normalized.Name,
		Protocol:         normalized.Protocol,
		Server:           normalized.Server,
		Port:             normalized.Port,
		Username:         normalized.Username,
		Password:         normalized.Password,
		RawURI:           normalized.RawURI,
		TagsJSON:         encodeStringSlice(normalized.Tags),
		Remark:           normalized.Remark,
		ChainNodeIDsJSON: encodeStringSlice(normalized.ChainNodeIDs),
		ChainMembersJSON: encodeChainMembers(normalized.ChainMembers),
		SubscriptionID:   req.SubscriptionID,
		GroupID:          req.GroupID,
		SourceKey:        req.SourceKey,
	}
	if existing == nil {
		return node, false, nil
	}
	node.ID = existing.ID
	return node, true, nil
}

func upsertSubscriptionNodeRows(ctx context.Context, tx model.DBTx, nodes []*tables.ProxyNodeTable) error {
	if len(nodes) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name",
			"protocol",
			"server",
			"port",
			"username",
			"password",
			"raw_uri",
			"tags_json",
			"remark",
			"chain_node_ids_json",
			"chain_members_json",
			"subscription_id",
			"group_id",
			"source_key",
			"updated_at",
		}),
	}).CreateInBatches(nodes, subscriptionSyncBatchSize).Error
}

func deleteSubscriptionNodesBulk(ctx context.Context, tx model.DBTx, nodeIDs []string) error {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}
	if err := ensureNodesNotReferencedByActiveChains(ctx, tx, nodeIDs); err != nil {
		return err
	}
	if err := cleanupNodeReferences(ctx, tx, nodeIDs); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("node_id IN ?", nodeIDs).Unscoped().Delete(&tables.ProxyNodeHealthTable{}).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("node_id IN ?", nodeIDs).Unscoped().Delete(&tables.ProxyNodeHealthHistoryTable{}).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Where("id IN ?", nodeIDs).Unscoped().Delete(&tables.ProxyNodeTable{}).Error
}

func ensureNodesNotReferencedByActiveChains(ctx context.Context, tx model.DBTx, nodeIDs []string) error {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}
	deleted := stringSet(nodeIDs)
	var nodes []*tables.ProxyNodeTable
	if err := tx.WithContext(ctx).Find(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if _, deletingNode := deleted[node.ID]; deletingNode {
			continue
		}
		if stringSlicesIntersect(chainNodeIDsFromMembers(chainMembersForNode(node)), nodeIDs) {
			return ErrInvalidChain
		}
	}
	return nil
}

func cleanupNodeReferences(ctx context.Context, tx model.DBTx, nodeIDs []string) error {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}

	var mappings []*tables.PortMappingTable
	if err := tx.WithContext(ctx).Find(&mappings).Error; err != nil {
		return err
	}
	for _, mapping := range mappings {
		nextNodeIDs := removeStrings(decodeStringSlice(mapping.NodeIDsJSON), nodeIDs)
		active := mapping.ActiveNodeID
		if normalizeStrategy(mapping.Strategy) != StrategyManual {
			active = ""
		} else if containsString(nodeIDs, active) {
			active = ""
			if len(nextNodeIDs) > 0 {
				active = nextNodeIDs[0]
			}
		}
		if err := tx.Model(mapping).Updates(map[string]any{
			"node_ids_json":  encodeStringSlice(nextNodeIDs),
			"active_node_id": active,
			"updated_at":     time.Now(),
		}).Error; err != nil {
			return err
		}
	}

	var groups []*tables.ProxyGroupTable
	if err := tx.WithContext(ctx).Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		nextNodeIDs := removeStrings(decodeStringSlice(group.NodeIDsJSON), nodeIDs)
		if err := tx.Model(group).Updates(map[string]any{
			"node_ids_json": encodeStringSlice(nextNodeIDs),
			"updated_at":    time.Now(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureSubscriptionGroupShell(ctx context.Context, tx model.DBTx, subscriptionID string, existing *tables.ProxyGroupTable, parsedGroup parsedSubscriptionGroup) (*tables.ProxyGroupTable, error) {
	if existing != nil {
		if err := tx.WithContext(ctx).Model(existing).Updates(map[string]any{
			"name":            parsedGroup.Name,
			"type":            GroupTypeSubscription,
			"strategy":        parsedGroup.Strategy,
			"subscription_id": subscriptionID,
			"source_key":      parsedGroup.SourceKey,
			"updated_at":      time.Now(),
		}).Error; err != nil {
			return nil, err
		}
		return GroupGet(ctx, tx, existing.ID)
	}
	group := &tables.ProxyGroupTable{
		Name:            parsedGroup.Name,
		Type:            GroupTypeSubscription,
		Strategy:        parsedGroup.Strategy,
		SubscriptionID:  subscriptionID,
		SourceKey:       parsedGroup.SourceKey,
		NodeIDsJSON:     encodeStringSlice(nil),
		GroupIDsJSON:    encodeStringSlice(nil),
		BuiltinTagsJSON: encodeStringSlice(nil),
		IncludesAll:     parsedGroup.IncludesAll,
		Filter:          parsedGroup.Filter,
	}
	if err := tx.WithContext(ctx).Create(group).Error; err != nil {
		return nil, err
	}
	return group, nil
}

func subscriptionGroupMembers(parsedGroup parsedSubscriptionGroup, nodeIDByName, groupIDByName map[string]string) ([]string, []string) {
	nodeIDs := make([]string, 0, len(parsedGroup.NodeNames))
	groupIDs := make([]string, 0, len(parsedGroup.GroupNames))
	for _, name := range parsedGroup.NodeNames {
		if id := nodeIDByName[name]; id != "" {
			nodeIDs = append(nodeIDs, id)
		}
	}
	for _, name := range parsedGroup.GroupNames {
		if id := groupIDByName[name]; id != "" {
			groupIDs = append(groupIDs, id)
		}
	}
	return uniqueNonEmpty(nodeIDs), uniqueNonEmpty(groupIDs)
}

func nodesBySubscriptionSource(ctx context.Context, tx model.DBTx, subscriptionID string) (map[string]*tables.ProxyNodeTable, error) {
	var nodes []*tables.ProxyNodeTable
	if err := tx.WithContext(ctx).Where("subscription_id = ?", subscriptionID).Find(&nodes).Error; err != nil {
		return nil, err
	}
	bySource := make(map[string]*tables.ProxyNodeTable, len(nodes))
	for _, node := range nodes {
		bySource[node.SourceKey] = node
	}
	return bySource, nil
}

func groupsBySubscriptionSource(ctx context.Context, tx model.DBTx, subscriptionID string) (map[string]*tables.ProxyGroupTable, error) {
	var groups []*tables.ProxyGroupTable
	if err := tx.WithContext(ctx).Where("subscription_id = ?", subscriptionID).Find(&groups).Error; err != nil {
		return nil, err
	}
	bySource := make(map[string]*tables.ProxyGroupTable, len(groups))
	for _, group := range groups {
		bySource[group.SourceKey] = group
	}
	return bySource, nil
}

func ensureSubscriptionRootGroup(ctx context.Context, tx model.DBTx, subscriptionName string, groupID string) (string, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID != "" {
		group, err := GroupGet(ctx, tx, groupID)
		if err != nil {
			return "", err
		}
		if group.Type == GroupTypeSubscription {
			return "", ErrInvalidGroup
		}
		return group.ID, nil
	}

	group := &tables.ProxyGroupTable{
		Name:            strings.TrimSpace(subscriptionName),
		Type:            GroupTypeManual,
		Strategy:        GroupStrategySelector,
		NodeIDsJSON:     encodeStringSlice(nil),
		GroupIDsJSON:    encodeStringSlice(nil),
		BuiltinTagsJSON: encodeStringSlice(nil),
	}
	if group.Name == "" {
		group.Name = "订阅分组"
	}
	if err := tx.WithContext(ctx).Create(group).Error; err != nil {
		return "", err
	}
	return group.ID, nil
}

func updateRootGroupReferences(ctx context.Context, tx model.DBTx, groupID string, nodeIDs []string, groupIDs []string) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}
	return tx.WithContext(ctx).Model(&tables.ProxyGroupTable{}).
		Where("id = ?", groupID).
		Updates(map[string]any{
			"node_ids_json":  encodeStringSlice(uniqueNonEmpty(nodeIDs)),
			"group_ids_json": encodeStringSlice(removeString(uniqueNonEmpty(groupIDs), groupID)),
			"updated_at":     time.Now(),
		}).Error
}

func normalizeSubscriptionRequest(req SubscriptionUpsertRequest) (*SubscriptionUpsertRequest, error) {
	normalized := req
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.URL = strings.TrimSpace(normalized.URL)
	normalized.GroupID = strings.TrimSpace(normalized.GroupID)
	normalized.Remark = strings.TrimSpace(normalized.Remark)
	if normalized.URL == "" {
		return nil, ErrInvalidSubscription
	}
	parsed, err := url.Parse(normalized.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidSubscription
	}
	if normalized.Name == "" {
		normalized.Name = parsed.Host
	}
	return &normalized, nil
}

func isLikelySubscriptionURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".txt") {
		return true
	}
	return strings.Contains(path, "/sub") || strings.Contains(path, "subscription")
}

func fetchSubscription(ctx context.Context, subscriptionURL string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, subscriptionURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "ProxyHub/1.0")
	client := &http.Client{Timeout: subscriptionHTTPTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("%w: status %d", ErrInvalidSubscription, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, subscriptionMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > subscriptionMaxBytes {
		return "", fmt.Errorf("%w: response too large", ErrInvalidSubscription)
	}
	return string(body), nil
}

func parseSubscription(raw string) (*parsedSubscription, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidSubscription
	}
	if strings.Contains(raw, "proxies:") || strings.Contains(raw, "proxy-groups:") {
		parsed, err := parseClashSubscription(raw)
		if err != nil {
			return nil, err
		}
		return &parsedSubscription{
			Nodes:        parsed.Nodes,
			Groups:       parsed.Groups,
			Failures:     parsed.Failures,
			PreviewItems: parsed.PreviewItems,
		}, nil
	}

	nodes := make([]parsedSubscriptionNode, 0)
	failures := make([]NodeImportFailure, 0)
	for _, rawURI := range normalizeImportURIs(NodeImportRequest{Raw: raw}) {
		parsed, err := ParseNodeURI(rawURI)
		if err != nil {
			failures = append(failures, NodeImportFailure{URI: rawURI, Message: err.Error()})
			continue
		}
		nodes = append(nodes, parsedSubscriptionNode{
			Name:      parsed.Name,
			SourceKey: sourceKey("node", parsed.Name),
			RawURI:    rawURI,
		})
	}
	previewItems := make([]NodeImportPreviewItem, 0, len(failures))
	for _, failure := range failures {
		previewItems = append(previewItems, previewFailureItem(failure.URI, failure.Message))
	}
	return &parsedSubscription{Nodes: nodes, Failures: failures, PreviewItems: previewItems}, nil
}

func parseClashSubscription(raw string) (*parsedClashConfig, error) {
	var config subscriptionConfig
	if err := yaml.Unmarshal([]byte(raw), &config); err != nil {
		return nil, err
	}
	result := &parsedClashConfig{
		Nodes:  make([]parsedSubscriptionNode, 0, len(config.Proxies)),
		Groups: make([]parsedSubscriptionGroup, 0, len(config.ProxyGroups)),
	}
	nodeNames := map[string]struct{}{}
	for _, proxy := range config.Proxies {
		name := stringFromMap(proxy, "name")
		if name == "" {
			result.Failures = append(result.Failures, NodeImportFailure{URI: "proxy", Message: "missing proxy name"})
			result.PreviewItems = append(result.PreviewItems, previewFailureItem("proxy", "missing proxy name"))
			continue
		}
		rawURI := clashProxyToURI(proxy)
		if rawURI == "" {
			message := fmt.Sprintf("%s: %s", ErrUnsupportedProtocol, stringFromMap(proxy, "type"))
			result.Failures = append(result.Failures, NodeImportFailure{URI: name, Message: message})
			result.PreviewItems = append(result.PreviewItems, NodeImportPreviewItem{
				Type:   ImportPreviewTypeFailure,
				Name:   name,
				Action: ImportPreviewActionFail,
				Reason: ImportPreviewReasonUnsupportedProtocol,
				Detail: message,
			})
			continue
		}
		result.Nodes = append(result.Nodes, parsedSubscriptionNode{
			Name:      name,
			SourceKey: sourceKey("node", name),
			RawURI:    rawURI,
		})
		nodeNames[name] = struct{}{}
	}
	rulesetPolicyGroups := clashRulesetPolicyGroups(config.Rules)
	groupNames := map[string]struct{}{}
	for _, group := range config.ProxyGroups {
		if name := stringFromMap(group, "name"); name != "" {
			if _, skip := rulesetPolicyGroups[name]; skip {
				continue
			}
			groupNames[name] = struct{}{}
		}
	}
	for _, group := range config.ProxyGroups {
		name := stringFromMap(group, "name")
		if name == "" {
			continue
		}
		if _, skip := rulesetPolicyGroups[name]; skip {
			result.PreviewItems = append(result.PreviewItems, NodeImportPreviewItem{
				Type:   ImportPreviewTypeGroup,
				Name:   name,
				Action: ImportPreviewActionSkip,
				Reason: ImportPreviewReasonRulesetPolicyGroup,
				Detail: "RULE-SET 规则命中的策略组不会导入",
			})
			continue
		}
		parsedGroup := parseClashProxyGroup(group, nodeNames, groupNames, result.Nodes)
		if parsedGroup.Name == "" {
			continue
		}
		if len(parsedGroup.NodeNames) == 0 && len(parsedGroup.GroupNames) > 0 && containsString(parsedGroup.BuiltinTags, constantDirect) {
			parsedGroup.BuiltinTags = removeString(parsedGroup.BuiltinTags, constantDirect)
			result.PreviewItems = append(result.PreviewItems, NodeImportPreviewItem{
				Type:   ImportPreviewTypeBuiltin,
				Name:   parsedGroup.Name + " / " + constantDirect,
				Action: ImportPreviewActionSkip,
				Reason: ImportPreviewReasonGroupOnlyDirect,
				Detail: "该节点组只引用其他分组，DIRECT 将自动忽略",
			})
		}
		result.Groups = append(result.Groups, parsedGroup)
	}
	return result, nil
}

func clashRulesetPolicyGroups(rules []string) map[string]struct{} {
	groups := make(map[string]struct{})
	for _, rawRule := range rules {
		parts := splitClashRule(rawRule)
		if len(parts) < 3 || !strings.EqualFold(parts[0], "RULE-SET") {
			continue
		}
		policy := strings.TrimSpace(parts[2])
		if policy == "" {
			continue
		}
		groups[policy] = struct{}{}
	}
	return groups
}

func splitClashRule(rule string) []string {
	values := strings.Split(rule, ",")
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			parts = append(parts, text)
		}
	}
	return parts
}

func parseClashProxyGroup(group map[string]any, nodeNames map[string]struct{}, groupNames map[string]struct{}, nodes []parsedSubscriptionNode) parsedSubscriptionGroup {
	name := stringFromMap(group, "name")
	strategy := normalizeClashGroupStrategy(stringFromMap(group, "type"))
	proxyNames := stringSliceFromMap(group, "proxies")
	includesAll := boolValueFromMap(group, "include-all", "include_all")
	filter := stringFromMap(group, "filter")

	nodeMembers := make([]string, 0)
	groupMembers := make([]string, 0)
	builtinTags := make([]string, 0)
	if includesAll {
		for _, node := range nodes {
			if subscriptionFilterMatch(filter, node.Name) {
				nodeMembers = append(nodeMembers, node.Name)
			}
		}
	}
	for _, proxyName := range proxyNames {
		switch proxyName {
		case constantDirect:
			builtinTags = append(builtinTags, constantDirect)
		case constantReject, constantRejectDrop:
			builtinTags = append(builtinTags, constantReject)
		default:
			if _, ok := nodeNames[proxyName]; ok {
				nodeMembers = append(nodeMembers, proxyName)
				continue
			}
			if _, ok := groupNames[proxyName]; ok {
				groupMembers = append(groupMembers, proxyName)
			}
		}
	}
	return parsedSubscriptionGroup{
		Name:        name,
		SourceKey:   sourceKey("group", name),
		Strategy:    strategy,
		NodeNames:   uniqueNonEmpty(nodeMembers),
		GroupNames:  uniqueNonEmpty(groupMembers),
		BuiltinTags: uniqueNonEmpty(builtinTags),
		IncludesAll: includesAll,
		Filter:      filter,
	}
}

func normalizeClashGroupStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GroupStrategyURLTest:
		return GroupStrategyURLTest
	default:
		return GroupStrategySelector
	}
}

func sourceKey(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}

func stringSliceFromMap(values map[string]any, key string) []string {
	value, ok := values[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				result = append(result, text)
			}
		}
		return result
	case []string:
		return uniqueNonEmpty(typed)
	default:
		return uniqueNonEmpty([]string{fmt.Sprint(typed)})
	}
}

func boolValueFromMap(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := boolFromMap(values, key)
		if ok {
			return value
		}
	}
	return false
}

func subscriptionFilterMatch(filter string, nodeName string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	matcher, err := regexp.Compile(filter)
	if err != nil {
		return strings.Contains(strings.ToLower(nodeName), strings.ToLower(filter))
	}
	return matcher.MatchString(nodeName)
}

func previewImportItem(itemType, name, action, reason, detail string) NodeImportPreviewItem {
	return NodeImportPreviewItem{
		Type:   itemType,
		Name:   name,
		Action: action,
		Reason: reason,
		Detail: detail,
	}
}

func previewFailureItem(name, message string) NodeImportPreviewItem {
	return NodeImportPreviewItem{
		Type:   ImportPreviewTypeFailure,
		Name:   name,
		Action: ImportPreviewActionFail,
		Reason: ImportPreviewReasonInvalidURI,
		Detail: message,
	}
}

func skippedPreviewCount(items []NodeImportPreviewItem) int {
	count := 0
	for _, item := range items {
		if item.Action == ImportPreviewActionSkip {
			count++
		}
	}
	return count
}
