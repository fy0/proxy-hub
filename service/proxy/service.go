package proxy

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func NodeCreate(ctx context.Context, tx model.DBTx, req NodeUpsertRequest) (*tables.ProxyNodeTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	normalized, err := normalizeNodeRequest(req)
	if err != nil {
		return nil, err
	}
	if err := normalizeNodeChainIDs(ctx, tx, "", normalized); err != nil {
		return nil, err
	}
	groupIDs, err := normalizeNodeGroupIDs(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	groupID := primaryGroupID(groupIDs)

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
		SubscriptionID:   strings.TrimSpace(req.SubscriptionID),
		GroupID:          groupID,
		SourceKey:        strings.TrimSpace(req.SourceKey),
	}
	if err := tx.Create(node).Error; err != nil {
		return nil, err
	}
	if err := syncNodeGroupMembership(ctx, tx, node.ID, nil, groupIDs); err != nil {
		return nil, err
	}
	return node, nil
}

func NodeUpdate(ctx context.Context, tx model.DBTx, id string, req NodeUpsertRequest) (*tables.ProxyNodeTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var node tables.ProxyNodeTable
	if err := tx.First(&node, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNodeNotFound
		}
		return nil, err
	}

	normalized, err := normalizeNodeRequest(req)
	if err != nil {
		return nil, err
	}
	if err := normalizeNodeChainIDs(ctx, tx, id, normalized); err != nil {
		return nil, err
	}
	groupIDs, err := normalizeNodeGroupIDs(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	previousGroupIDs, err := nodeGroupIDs(ctx, tx, node.ID, node.GroupID)
	if err != nil {
		return nil, err
	}
	if err := syncNodeGroupMembership(ctx, tx, node.ID, previousGroupIDs, groupIDs); err != nil {
		return nil, err
	}
	groupID := primaryGroupID(groupIDs)

	if err := tx.Model(&node).Updates(map[string]any{
		"name":                normalized.Name,
		"protocol":            normalized.Protocol,
		"server":              normalized.Server,
		"port":                normalized.Port,
		"username":            normalized.Username,
		"password":            normalized.Password,
		"raw_uri":             normalized.RawURI,
		"tags_json":           encodeStringSlice(normalized.Tags),
		"remark":              normalized.Remark,
		"chain_node_ids_json": encodeStringSlice(normalized.ChainNodeIDs),
		"chain_members_json":  encodeChainMembers(normalized.ChainMembers),
		"group_id":            groupID,
		"updated_at":          time.Now(),
	}).Error; err != nil {
		return nil, err
	}

	return NodeGet(ctx, tx, id)
}

func NodeGet(ctx context.Context, tx model.DBTx, id string) (*tables.ProxyNodeTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var node tables.ProxyNodeTable
	if err := tx.First(&node, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNodeNotFound
		}
		return nil, err
	}
	return &node, nil
}

func NodeList(ctx context.Context, tx model.DBTx) ([]*tables.ProxyNodeTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var nodes []*tables.ProxyNodeTable
	if err := tx.Order("created_at DESC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func NodeListPaged(ctx context.Context, tx model.DBTx, req NodeListRequest, page, size int) ([]*tables.ProxyNodeTable, int64, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	page, size = normalizePage(page, size)

	query, err := applyNodeListRequest(ctx, tx.Model(&tables.ProxyNodeTable{}), tx, req)
	if err != nil {
		return nil, 0, err
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var nodes []*tables.ProxyNodeTable
	if err := query.
		Order("created_at DESC").
		Limit(size).
		Offset((page - 1) * size).
		Find(&nodes).Error; err != nil {
		return nil, 0, err
	}
	return nodes, total, nil
}

func NodeCount(ctx context.Context, tx model.DBTx, req NodeListRequest) (int64, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	query, err := applyNodeListRequest(ctx, tx.Model(&tables.ProxyNodeTable{}), tx, req)
	if err != nil {
		return 0, err
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func NodeDelete(ctx context.Context, tx model.DBTx, id string) error {
	if tx != nil {
		return nodeDeleteInTx(ctx, tx, id)
	}
	return model.Transaction(ctx, func(inner model.DBTx) error {
		return nodeDeleteInTx(ctx, inner, id)
	})
}

func nodeDeleteInTx(ctx context.Context, tx model.DBTx, id string) error {
	tx = model.GetTx(tx).WithContext(ctx)

	var node tables.ProxyNodeTable
	if err := tx.First(&node, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNodeNotFound
		}
		return err
	}
	if err := ensureNodeNotReferencedByChains(ctx, tx, id); err != nil {
		return err
	}

	var mappings []*tables.PortMappingTable
	if err := tx.Find(&mappings).Error; err != nil {
		return err
	}
	for _, mapping := range mappings {
		nodeIDs := removeString(decodeStringSlice(mapping.NodeIDsJSON), id)
		active := mapping.ActiveNodeID
		if normalizeStrategy(mapping.Strategy) != StrategyManual {
			active = ""
		} else if active == id {
			active = ""
			if len(nodeIDs) > 0 {
				active = nodeIDs[0]
			}
		}
		if err := tx.Model(mapping).Updates(map[string]any{
			"node_ids_json":  encodeStringSlice(nodeIDs),
			"active_node_id": active,
			"updated_at":     time.Now(),
		}).Error; err != nil {
			return err
		}
	}

	var groups []*tables.ProxyGroupTable
	if err := tx.Find(&groups).Error; err != nil {
		return err
	}
	for _, group := range groups {
		nodeIDs := removeString(decodeStringSlice(group.NodeIDsJSON), id)
		if err := tx.Model(group).Updates(map[string]any{
			"node_ids_json": encodeStringSlice(nodeIDs),
			"updated_at":    time.Now(),
		}).Error; err != nil {
			return err
		}
	}

	if err := tx.Where("node_id = ?", id).Unscoped().Delete(&tables.ProxyNodeHealthTable{}).Error; err != nil {
		return err
	}
	if err := tx.Where("node_id = ?", id).Unscoped().Delete(&tables.ProxyNodeHealthHistoryTable{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Delete(&node).Error
}

func NodeImport(ctx context.Context, tx model.DBTx, req NodeImportRequest) (*NodeImportResult, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	req.GroupID = strings.TrimSpace(req.GroupID)
	if req.GroupID != "" {
		if _, err := GroupGet(ctx, tx, req.GroupID); err != nil {
			return nil, err
		}
	}

	if raw := clashImportRaw(req); raw != "" {
		return importManualClashRaw(ctx, tx, req, raw)
	}

	uris, fetchFailures := normalizeImportURIsWithFetch(ctx, req)
	result := &NodeImportResult{Total: len(uris) + len(fetchFailures), Failures: fetchFailures, Skipped: len(fetchFailures)}
	for _, failure := range fetchFailures {
		result.PreviewItems = append(result.PreviewItems, NodeImportPreviewItem{
			Type:   ImportPreviewTypeFailure,
			Name:   failure.URI,
			Action: ImportPreviewActionFail,
			Reason: ImportPreviewReasonFetchFailed,
			Detail: failure.Message,
		})
	}
	for _, rawURI := range uris {
		parsed, err := ParseNodeURI(rawURI)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: rawURI, Message: err.Error()})
			result.PreviewItems = append(result.PreviewItems, previewFailureItem(rawURI, err.Error()))
			result.Skipped++
			continue
		}
		parsed.GroupID = req.GroupID
		node, err := NodeCreate(ctx, tx, *parsed)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: rawURI, Message: err.Error()})
			result.PreviewItems = append(result.PreviewItems, previewFailureItem(rawURI, err.Error()))
			result.Skipped++
			continue
		}
		result.Items = append(result.Items, ToNodeDTO(node))
		result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeNode, node.Name, ImportPreviewActionImport, ImportPreviewReasonImport, "节点将导入"))
	}
	result.Imported = len(result.Items)
	result.Failed = len(result.Failures)
	return result, nil
}

func NodeImportPreview(ctx context.Context, tx model.DBTx, req NodeImportRequest) (*NodeImportResult, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	req.GroupID = strings.TrimSpace(req.GroupID)
	if req.GroupID != "" {
		if _, err := GroupGet(ctx, tx, req.GroupID); err != nil {
			return nil, err
		}
	}
	if raw := clashImportRaw(req); raw != "" {
		return PreviewImportRaw(ctx, tx, raw)
	}

	uris, fetchFailures := normalizeImportURIsWithFetch(ctx, req)
	result := &NodeImportResult{
		Total:    len(uris) + len(fetchFailures),
		Failures: fetchFailures,
		Skipped:  len(fetchFailures),
	}
	for _, failure := range fetchFailures {
		result.PreviewItems = append(result.PreviewItems, NodeImportPreviewItem{
			Type:   ImportPreviewTypeFailure,
			Name:   failure.URI,
			Action: ImportPreviewActionFail,
			Reason: ImportPreviewReasonFetchFailed,
			Detail: failure.Message,
		})
	}
	for _, rawURI := range uris {
		parsed, err := ParseNodeURI(rawURI)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: rawURI, Message: err.Error()})
			result.PreviewItems = append(result.PreviewItems, previewFailureItem(rawURI, err.Error()))
			result.Skipped++
			continue
		}
		result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeNode, parsed.Name, ImportPreviewActionImport, ImportPreviewReasonImport, "节点将导入"))
	}
	result.Failed = len(result.Failures)
	return result, nil
}

func importManualClashRaw(ctx context.Context, tx model.DBTx, req NodeImportRequest, raw string) (*NodeImportResult, error) {
	parsed, err := parseClashSubscription(raw)
	if err != nil {
		return nil, err
	}
	result := &NodeImportResult{
		Total:        len(parsed.Nodes) + len(parsed.Groups) + len(parsed.Failures) + skippedPreviewCount(parsed.PreviewItems),
		Failures:     append([]NodeImportFailure(nil), parsed.Failures...),
		PreviewItems: append([]NodeImportPreviewItem(nil), parsed.PreviewItems...),
		Skipped:      len(parsed.Failures) + skippedPreviewCount(parsed.PreviewItems),
	}

	nodeIDByName := map[string]string{}
	for _, parsedNode := range parsed.Nodes {
		nodeReq, err := ParseNodeURI(parsedNode.RawURI)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: parsedNode.Name, Message: err.Error()})
			result.PreviewItems = append(result.PreviewItems, previewFailureItem(parsedNode.Name, err.Error()))
			result.Skipped++
			continue
		}
		nodeReq.GroupID = req.GroupID
		nodeReq.SourceKey = parsedNode.SourceKey
		existing := findManualNodeBySourceOrName(ctx, tx, parsedNode.SourceKey, nodeReq.Name)
		node, existed, err := upsertManualImportNode(ctx, tx, existing, *nodeReq)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: parsedNode.Name, Message: err.Error()})
			result.PreviewItems = append(result.PreviewItems, previewFailureItem(parsedNode.Name, err.Error()))
			result.Skipped++
			continue
		}
		if existed {
			result.Updated++
			result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeNode, parsedNode.Name, ImportPreviewActionUpdate, ImportPreviewReasonUpdate, "节点将更新"))
		} else {
			result.Imported++
			result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeNode, parsedNode.Name, ImportPreviewActionImport, ImportPreviewReasonImport, "节点将导入"))
		}
		result.Items = append(result.Items, ToNodeDTO(node))
		nodeIDByName[node.Name] = node.ID
		nodeIDByName[parsedNode.Name] = node.ID
	}

	groupIDByName := map[string]string{}
	for _, parsedGroup := range parsed.Groups {
		existing := findManualGroupBySourceOrName(ctx, tx, parsedGroup.SourceKey, parsedGroup.Name)
		group, existed, err := upsertManualImportGroupShell(ctx, tx, existing, parsedGroup)
		if err != nil {
			return result, err
		}
		if existed {
			result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeGroup, parsedGroup.Name, ImportPreviewActionUpdate, ImportPreviewReasonUpdate, "节点组将更新"))
		} else {
			result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeGroup, parsedGroup.Name, ImportPreviewActionImport, ImportPreviewReasonImport, "节点组将导入"))
		}
		groupIDByName[group.Name] = group.ID
	}

	for _, parsedGroup := range parsed.Groups {
		groupID := groupIDByName[parsedGroup.Name]
		if groupID == "" {
			continue
		}
		group, err := GroupGet(ctx, tx, groupID)
		if err != nil {
			return result, err
		}
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
		refreshed, err := GroupGet(ctx, tx, group.ID)
		if err != nil {
			return result, err
		}
		result.Groups = append(result.Groups, ToGroupDTO(refreshed))
	}

	if req.GroupID != "" {
		groupIDs := make([]string, 0, len(result.Groups))
		for _, group := range result.Groups {
			if group != nil {
				groupIDs = append(groupIDs, group.ID)
			}
		}
		nodeIDs := make([]string, 0, len(result.Items))
		for _, node := range result.Items {
			if node != nil {
				nodeIDs = append(nodeIDs, node.ID)
			}
		}
		if err := appendManualImportToGroup(ctx, tx, req.GroupID, nodeIDs, groupIDs); err != nil {
			return result, err
		}
	}

	result.Failed = len(result.Failures)
	return result, nil
}

func PreviewImportRaw(ctx context.Context, tx model.DBTx, raw string) (*NodeImportResult, error) {
	parsed, err := parseSubscription(raw)
	if err != nil {
		return nil, err
	}
	result := &NodeImportResult{
		Total:        len(parsed.Nodes) + len(parsed.Groups) + len(parsed.Failures) + skippedPreviewCount(parsed.PreviewItems),
		Failures:     append([]NodeImportFailure(nil), parsed.Failures...),
		PreviewItems: append([]NodeImportPreviewItem(nil), parsed.PreviewItems...),
		Skipped:      len(parsed.Failures) + skippedPreviewCount(parsed.PreviewItems),
		Failed:       len(parsed.Failures),
	}
	for _, node := range parsed.Nodes {
		action := ImportPreviewActionImport
		reason := ImportPreviewReasonImport
		detail := "节点将导入"
		if manualNodeExists(ctx, tx, node.SourceKey, node.Name) {
			action = ImportPreviewActionUpdate
			reason = ImportPreviewReasonUpdate
			detail = "节点将更新"
		}
		result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeNode, node.Name, action, reason, detail))
	}
	for _, group := range parsed.Groups {
		action := ImportPreviewActionImport
		reason := ImportPreviewReasonImport
		detail := "节点组将导入"
		if manualGroupExists(ctx, tx, group.SourceKey, group.Name) {
			action = ImportPreviewActionUpdate
			reason = ImportPreviewReasonUpdate
			detail = "节点组将更新"
		}
		result.PreviewItems = append(result.PreviewItems, previewImportItem(ImportPreviewTypeGroup, group.Name, action, reason, detail))
	}
	return result, nil
}

func clashImportRaw(req NodeImportRequest) string {
	if raw := strings.TrimSpace(req.Raw); isClashSubscriptionRaw(raw) {
		return raw
	}
	for _, uri := range req.URIs {
		if raw := strings.TrimSpace(uri); isClashSubscriptionRaw(raw) {
			return raw
		}
	}
	return ""
}

func isClashSubscriptionRaw(raw string) bool {
	return strings.Contains(raw, "proxies:") || strings.Contains(raw, "proxy-groups:")
}

func findManualNodeBySourceOrName(ctx context.Context, tx model.DBTx, sourceKey, name string) *tables.ProxyNodeTable {
	var node tables.ProxyNodeTable
	query := tx.WithContext(ctx).Where("subscription_id = '' AND source_key = ?", sourceKey)
	if err := query.First(&node).Error; err == nil {
		return &node
	}
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if err := tx.WithContext(ctx).Where("subscription_id = '' AND name = ?", name).First(&node).Error; err == nil {
		return &node
	}
	return nil
}

func findManualGroupBySourceOrName(ctx context.Context, tx model.DBTx, sourceKey, name string) *tables.ProxyGroupTable {
	var group tables.ProxyGroupTable
	if err := tx.WithContext(ctx).Where("type = ? AND subscription_id = '' AND source_key = ?", GroupTypeManual, sourceKey).First(&group).Error; err == nil {
		return &group
	}
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if err := tx.WithContext(ctx).Where("type = ? AND subscription_id = '' AND name = ?", GroupTypeManual, name).First(&group).Error; err == nil {
		return &group
	}
	return nil
}

func manualNodeExists(ctx context.Context, tx model.DBTx, sourceKey, name string) bool {
	return findManualNodeBySourceOrName(ctx, model.GetTx(tx), sourceKey, name) != nil
}

func manualGroupExists(ctx context.Context, tx model.DBTx, sourceKey, name string) bool {
	return findManualGroupBySourceOrName(ctx, model.GetTx(tx), sourceKey, name) != nil
}

func upsertManualImportNode(ctx context.Context, tx model.DBTx, existing *tables.ProxyNodeTable, req NodeUpsertRequest) (*tables.ProxyNodeTable, bool, error) {
	req.SubscriptionID = ""
	if existing == nil {
		node, err := NodeCreate(ctx, tx, req)
		return node, false, err
	}
	normalized, err := normalizeNodeRequest(req)
	if err != nil {
		return nil, true, err
	}
	if err := normalizeNodeChainIDs(ctx, tx, existing.ID, normalized); err != nil {
		return nil, true, err
	}
	previousGroupID := strings.TrimSpace(existing.GroupID)
	if previousGroupID != "" && previousGroupID != req.GroupID {
		if err := removeNodesFromGroupMembership(ctx, tx, previousGroupID, []string{existing.ID}); err != nil {
			return nil, true, err
		}
	}
	if req.GroupID != "" {
		if err := addNodesToGroupMembership(ctx, tx, req.GroupID, []string{existing.ID}); err != nil {
			return nil, true, err
		}
	}
	if err := tx.WithContext(ctx).Model(existing).Updates(map[string]any{
		"name":                normalized.Name,
		"protocol":            normalized.Protocol,
		"server":              normalized.Server,
		"port":                normalized.Port,
		"username":            normalized.Username,
		"password":            normalized.Password,
		"raw_uri":             normalized.RawURI,
		"tags_json":           encodeStringSlice(normalized.Tags),
		"remark":              normalized.Remark,
		"chain_node_ids_json": encodeStringSlice(normalized.ChainNodeIDs),
		"chain_members_json":  encodeChainMembers(normalized.ChainMembers),
		"group_id":            req.GroupID,
		"source_key":          req.SourceKey,
		"updated_at":          time.Now(),
	}).Error; err != nil {
		return nil, true, err
	}
	node, err := NodeGet(ctx, tx, existing.ID)
	return node, true, err
}

func upsertManualImportGroupShell(ctx context.Context, tx model.DBTx, existing *tables.ProxyGroupTable, parsedGroup parsedSubscriptionGroup) (*tables.ProxyGroupTable, bool, error) {
	if existing != nil {
		if err := tx.WithContext(ctx).Model(existing).Updates(map[string]any{
			"name":         parsedGroup.Name,
			"type":         GroupTypeManual,
			"strategy":     parsedGroup.Strategy,
			"source_key":   parsedGroup.SourceKey,
			"includes_all": parsedGroup.IncludesAll,
			"filter":       parsedGroup.Filter,
			"updated_at":   time.Now(),
		}).Error; err != nil {
			return nil, true, err
		}
		group, err := GroupGet(ctx, tx, existing.ID)
		return group, true, err
	}
	group := &tables.ProxyGroupTable{
		Name:            parsedGroup.Name,
		Type:            GroupTypeManual,
		Strategy:        parsedGroup.Strategy,
		SourceKey:       parsedGroup.SourceKey,
		NodeIDsJSON:     encodeStringSlice(nil),
		GroupIDsJSON:    encodeStringSlice(nil),
		BuiltinTagsJSON: encodeStringSlice(nil),
		IncludesAll:     parsedGroup.IncludesAll,
		Filter:          parsedGroup.Filter,
	}
	if err := tx.WithContext(ctx).Create(group).Error; err != nil {
		return nil, false, err
	}
	return group, false, nil
}

func appendManualImportToGroup(ctx context.Context, tx model.DBTx, targetGroupID string, nodeIDs, groupIDs []string) error {
	targetGroupID = strings.TrimSpace(targetGroupID)
	if targetGroupID == "" {
		return nil
	}
	var group tables.ProxyGroupTable
	if err := tx.WithContext(ctx).First(&group, "id = ?", targetGroupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupNotFound
		}
		return err
	}
	nextNodeIDs := uniqueNonEmpty(append(decodeStringSlice(group.NodeIDsJSON), nodeIDs...))
	nextGroupIDs := uniqueNonEmpty(append(decodeStringSlice(group.GroupIDsJSON), removeString(groupIDs, targetGroupID)...))
	return tx.WithContext(ctx).Model(&group).Updates(map[string]any{
		"node_ids_json":  encodeStringSlice(nextNodeIDs),
		"group_ids_json": encodeStringSlice(nextGroupIDs),
		"updated_at":     time.Now(),
	}).Error
}

func MappingCreate(ctx context.Context, tx model.DBTx, req MappingUpsertRequest) (*tables.PortMappingTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	normalized, err := normalizeMappingRequest(ctx, tx, "", req)
	if err != nil {
		return nil, err
	}
	order, err := nextMappingOrder(ctx, tx)
	if err != nil {
		return nil, err
	}

	mapping := &tables.PortMappingTable{
		Enabled:                    normalized.Enabled,
		ListenAddress:              normalized.ListenAddress,
		ListenPort:                 normalized.ListenPort,
		Order:                      order,
		OutboundProtocol:           normalized.OutboundProtocol,
		Username:                   normalized.Username,
		Password:                   normalized.Password,
		Strategy:                   normalized.Strategy,
		NodeIDsJSON:                encodeStringSlice(normalized.NodeIDs),
		ActiveNodeID:               valueOrEmpty(normalized.ActiveNodeID),
		GroupIDsJSON:               encodeStringSlice(normalized.GroupIDs),
		GroupStrategyOverridesJSON: encodeGroupStrategyOverrides(normalized.GroupStrategyOverrides),
		ActiveGroupID:              valueOrEmpty(normalized.ActiveGroupID),
		Remark:                     normalized.Remark,
	}
	if err := tx.Create(mapping).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrListenPortTaken
		}
		return nil, err
	}
	return mapping, nil
}

func MappingUpdate(ctx context.Context, tx model.DBTx, id string, req MappingUpsertRequest) (*tables.PortMappingTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var mapping tables.PortMappingTable
	if err := tx.First(&mapping, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMappingNotFound
		}
		return nil, err
	}

	normalized, err := normalizeMappingRequest(ctx, tx, id, req)
	if err != nil {
		return nil, err
	}

	if err := tx.Model(&mapping).Updates(map[string]any{
		"enabled":                       normalized.Enabled,
		"listen_address":                normalized.ListenAddress,
		"listen_port":                   normalized.ListenPort,
		"outbound_protocol":             normalized.OutboundProtocol,
		"username":                      normalized.Username,
		"password":                      normalized.Password,
		"strategy":                      normalized.Strategy,
		"node_ids_json":                 encodeStringSlice(normalized.NodeIDs),
		"active_node_id":                valueOrEmpty(normalized.ActiveNodeID),
		"group_ids_json":                encodeStringSlice(normalized.GroupIDs),
		"group_strategy_overrides_json": encodeGroupStrategyOverrides(normalized.GroupStrategyOverrides),
		"active_group_id":               valueOrEmpty(normalized.ActiveGroupID),
		"remark":                        normalized.Remark,
		"updated_at":                    time.Now(),
	}).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrListenPortTaken
		}
		return nil, err
	}

	return MappingGet(ctx, tx, id)
}

func MappingSwitch(ctx context.Context, tx model.DBTx, id string, req MappingSwitchRequest) (*tables.PortMappingTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var mapping tables.PortMappingTable
	if err := tx.First(&mapping, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMappingNotFound
		}
		return nil, err
	}
	if normalizeStrategy(mapping.Strategy) != StrategyManual {
		return nil, ErrInvalidMappingSwitch
	}

	targetType := strings.ToLower(strings.TrimSpace(req.TargetType))
	targetID := strings.TrimSpace(req.TargetID)
	if targetID == "" {
		return nil, ErrInvalidMappingSwitch
	}

	updates := map[string]any{
		"updated_at": time.Now(),
	}
	switch targetType {
	case MappingSwitchTargetNode:
		if !containsString(decodeStringSlice(mapping.NodeIDsJSON), targetID) {
			return nil, ErrInvalidMappingSwitch
		}
		updates["active_node_id"] = targetID
		updates["active_group_id"] = ""
	case MappingSwitchTargetGroup:
		if !containsString(decodeStringSlice(mapping.GroupIDsJSON), targetID) {
			return nil, ErrInvalidMappingSwitch
		}
		updates["active_node_id"] = ""
		updates["active_group_id"] = targetID
	default:
		return nil, ErrInvalidMappingSwitch
	}

	if err := tx.Model(&mapping).Updates(updates).Error; err != nil {
		return nil, err
	}
	return MappingGet(ctx, tx, id)
}

func MappingGet(ctx context.Context, tx model.DBTx, id string) (*tables.PortMappingTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var mapping tables.PortMappingTable
	if err := tx.First(&mapping, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMappingNotFound
		}
		return nil, err
	}
	return &mapping, nil
}

func MappingList(ctx context.Context, tx model.DBTx) ([]*tables.PortMappingTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var mappings []*tables.PortMappingTable
	if err := tx.Order(mappingOrderClause()).Find(&mappings).Error; err != nil {
		return nil, err
	}
	return mappings, nil
}

func MappingDelete(ctx context.Context, tx model.DBTx, id string) error {
	tx = model.GetTx(tx).WithContext(ctx)

	var mapping tables.PortMappingTable
	if err := tx.First(&mapping, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMappingNotFound
		}
		return err
	}
	return tx.Unscoped().Delete(&mapping).Error
}

func StateSnapshot(ctx context.Context, tx model.DBTx, options ...StateSnapshotOptions) (*StateSnapshotDTO, error) {
	opts := StateSnapshotOptions{IncludeNodes: true, IncludeGroupMembers: true}
	if len(options) > 0 {
		opts = options[0]
	}

	groups, err := GroupList(ctx, tx)
	if err != nil {
		return nil, err
	}
	nodeTotal, err := NodeCount(ctx, tx, NodeListRequest{})
	if err != nil {
		return nil, err
	}
	defaultTotal, err := NodeCount(ctx, tx, NodeListRequest{DefaultOnly: true})
	if err != nil {
		return nil, err
	}
	subscriptions, err := SubscriptionList(ctx, tx)
	if err != nil {
		return nil, err
	}
	mappings, err := MappingList(ctx, tx)
	if err != nil {
		return nil, err
	}
	groupDTOs := ToGroupDTOs(groups)
	if !opts.IncludeGroupMembers {
		for _, group := range groupDTOs {
			if group != nil {
				group.NodeIDs = nil
				group.GroupIDs = nil
				group.BuiltinTags = nil
			}
		}
	}

	var nodeDTOs []*ProxyNodeDTO
	if opts.IncludeNodes {
		nodes, err := NodeList(ctx, tx)
		if err != nil {
			return nil, err
		}
		healthByNodeID := NodeHealthMap(ctx, tx, nodeIDsFromNodes(nodes))
		nodeDTOs = ToNodeDTOsWithHealthAndGroups(nodes, healthByNodeID, groups)
	}

	snapshot := &StateSnapshotDTO{
		Nodes:         nodeDTOs,
		Groups:        groupDTOs,
		Subscriptions: ToSubscriptionDTOs(subscriptions),
		Mappings:      ToMappingDTOs(mappings),
		Runtime:       RuntimeStatusGet(),
		LastSavedAt:   time.Now(),
		NodeTotal:     nodeTotal,
		DefaultTotal:  defaultTotal,
	}
	return snapshot, nil
}

func nodeIDsFromNodes(nodes []*tables.ProxyNodeTable) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func nextMappingOrder(ctx context.Context, tx model.DBTx) (int64, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var latest tables.PortMappingTable
	err := tx.Order(mappingOrderDescClause()).First(&latest).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 1, nil
		}
		return 0, err
	}
	return latest.Order + 1, nil
}

func mappingOrderClause() clause.OrderBy {
	return clause.OrderBy{
		Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "order"}},
			{Column: clause.Column{Name: "created_at"}},
		},
	}
}

func mappingOrderDescClause() clause.OrderBy {
	return clause.OrderBy{
		Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "order"}, Desc: true},
			{Column: clause.Column{Name: "created_at"}, Desc: true},
		},
	}
}

func findNodesByIDs(ctx context.Context, tx model.DBTx, ids []string) ([]*tables.ProxyNodeTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return []*tables.ProxyNodeTable{}, nil
	}

	var nodes []*tables.ProxyNodeTable
	if err := tx.Where("id IN ?", ids).Find(&nodes).Error; err != nil {
		return nil, err
	}

	byID := make(map[string]*tables.ProxyNodeTable, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	ordered := make([]*tables.ProxyNodeTable, 0, len(nodes))
	for _, id := range ids {
		if node := byID[id]; node != nil {
			ordered = append(ordered, node)
		}
	}
	return ordered, nil
}

func findNodesByGroupOrIDs(ctx context.Context, tx model.DBTx, groupID string, ids []string) ([]*tables.ProxyNodeTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	groupID = strings.TrimSpace(groupID)
	ids = uniqueNonEmpty(ids)
	if groupID == "" {
		return findNodesByIDs(ctx, tx, ids)
	}

	var nodes []*tables.ProxyNodeTable
	query := tx.Where("group_id = ?", groupID)
	if len(ids) > 0 {
		query = tx.Where("id IN ? OR group_id = ?", ids, groupID)
	}
	if err := query.Order("created_at DESC").Find(&nodes).Error; err != nil {
		return nil, err
	}

	byID := make(map[string]*tables.ProxyNodeTable, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	ordered := make([]*tables.ProxyNodeTable, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, id := range ids {
		node := byID[id]
		if node == nil {
			continue
		}
		ordered = append(ordered, node)
		seen[node.ID] = struct{}{}
	}
	for _, node := range nodes {
		if _, ok := seen[node.ID]; ok {
			continue
		}
		ordered = append(ordered, node)
	}
	return ordered, nil
}

func normalizePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func applyNodeListRequest(ctx context.Context, query *gorm.DB, tx model.DBTx, req NodeListRequest) (*gorm.DB, error) {
	query = query.WithContext(ctx)
	req.Keyword = strings.TrimSpace(req.Keyword)
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.IDs = uniqueNonEmpty(req.IDs)

	if len(req.IDs) > 0 {
		query = query.Where("id IN ?", req.IDs)
	}
	if req.PhysicalOnly {
		query = query.Where("protocol <> ?", ProtocolChain)
	}
	if req.Keyword != "" {
		keyword := strings.ToLower(req.Keyword)
		nodeIDKeyword := strings.TrimPrefix(keyword, "node-")
		pattern := "%" + keyword + "%"
		nodeIDPattern := "%" + nodeIDKeyword + "%"
		if req.NameOnly {
			query = query.Where("lower(name) LIKE ?", pattern)
		} else {
			query = query.Where(
				"lower(id) LIKE ? OR lower(id) LIKE ? OR lower(name) LIKE ? OR lower(protocol) LIKE ? OR lower(server) LIKE ? OR lower(username) LIKE ? OR lower(remark) LIKE ? OR lower(tags_json) LIKE ?",
				pattern, nodeIDPattern, pattern, pattern, pattern, pattern, pattern, pattern,
			)
		}
	}

	if req.GroupID != "" || req.DefaultOnly {
		groups, err := GroupList(ctx, tx)
		if err != nil {
			return nil, err
		}
		if req.DefaultOnly {
			grouped := make([]string, 0)
			for _, group := range groups {
				grouped = append(grouped, decodeStringSlice(group.NodeIDsJSON)...)
			}
			query = query.Where("group_id = ''")
			if grouped = uniqueNonEmpty(grouped); len(grouped) > 0 {
				query = query.Where("id NOT IN ?", grouped)
			}
		}
		if req.GroupID != "" {
			var memberIDs []string
			found := false
			for _, group := range groups {
				if group == nil || group.ID != req.GroupID {
					continue
				}
				found = true
				memberIDs = decodeStringSlice(group.NodeIDsJSON)
				break
			}
			if !found {
				return nil, ErrGroupNotFound
			}
			memberIDs = uniqueNonEmpty(memberIDs)
			if len(memberIDs) == 0 {
				query = query.Where("group_id = ?", req.GroupID)
			} else {
				query = query.Where("group_id = ? OR id IN ?", req.GroupID, memberIDs)
			}
		}
	}

	return query, nil
}

func findGroupsByIDs(ctx context.Context, tx model.DBTx, ids []string) ([]*tables.ProxyGroupTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	ids = uniqueNonEmpty(ids)
	if len(ids) == 0 {
		return []*tables.ProxyGroupTable{}, nil
	}

	var groups []*tables.ProxyGroupTable
	if err := tx.Where("id IN ?", ids).Find(&groups).Error; err != nil {
		return nil, err
	}

	byID := make(map[string]*tables.ProxyGroupTable, len(groups))
	for _, group := range groups {
		byID[group.ID] = group
	}
	ordered := make([]*tables.ProxyGroupTable, 0, len(groups))
	for _, id := range ids {
		if group := byID[id]; group != nil {
			ordered = append(ordered, group)
		}
	}
	return ordered, nil
}

func normalizeNodeGroupIDs(ctx context.Context, tx model.DBTx, req NodeUpsertRequest) ([]string, error) {
	groupIDs := uniqueNonEmpty(req.GroupIDs)
	if len(groupIDs) == 0 {
		groupIDs = stringSliceOrEmpty(strings.TrimSpace(req.GroupID))
	}
	if len(groupIDs) == 0 {
		return []string{}, nil
	}
	groups, err := findGroupsByIDs(ctx, tx, groupIDs)
	if err != nil {
		return nil, err
	}
	if len(groups) != len(groupIDs) {
		return nil, ErrGroupNotFound
	}
	normalized := make([]string, 0, len(groups))
	for _, group := range groups {
		normalized = append(normalized, group.ID)
	}
	return normalized, nil
}

func nodeGroupIDs(ctx context.Context, tx model.DBTx, nodeID string, legacyGroupID string) ([]string, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	var groups []*tables.ProxyGroupTable
	if err := tx.Find(&groups).Error; err != nil {
		return nil, err
	}
	return groupIDsForNodeFromGroups(nodeID, legacyGroupID, groups), nil
}

func primaryGroupID(groupIDs []string) string {
	groupIDs = uniqueNonEmpty(groupIDs)
	if len(groupIDs) == 0 {
		return ""
	}
	return groupIDs[0]
}

func syncNodeGroupMembership(ctx context.Context, tx model.DBTx, nodeID string, previousGroupIDs []string, nextGroupIDs []string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	for _, groupID := range differenceStrings(previousGroupIDs, nextGroupIDs) {
		if err := removeNodesFromGroupMembership(ctx, tx, groupID, []string{nodeID}); err != nil {
			return err
		}
	}
	for _, groupID := range differenceStrings(nextGroupIDs, previousGroupIDs) {
		if err := addNodesToGroupMembership(ctx, tx, groupID, []string{nodeID}); err != nil {
			return err
		}
	}
	return nil
}

func normalizeNodeRequest(req NodeUpsertRequest) (*NodeUpsertRequest, error) {
	if strings.TrimSpace(req.RawURI) != "" {
		parsed, err := ParseNodeURI(req.RawURI)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Name) != "" {
			parsed.Name = req.Name
		}
		if strings.TrimSpace(req.Remark) != "" {
			parsed.Remark = req.Remark
		}
		if len(req.Tags) > 0 {
			parsed.Tags = req.Tags
		}
		req = *parsed
	}

	normalized := req
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.Protocol = normalizeProtocol(normalized.Protocol)
	normalized.Server = strings.TrimSpace(normalized.Server)
	normalized.Username = strings.TrimSpace(normalized.Username)
	normalized.Password = strings.TrimSpace(normalized.Password)
	normalized.Remark = strings.TrimSpace(normalized.Remark)
	normalized.Tags = cleanTags(normalized.Tags, normalized.Protocol)
	normalized.ChainMembers = normalizeChainMembers(normalized.ChainMembers)
	if len(normalized.ChainMembers) == 0 {
		normalized.ChainMembers = chainMembersFromNodeIDs(normalized.ChainNodeIDs)
	}
	normalized.ChainNodeIDs = chainNodeIDsFromMembers(normalized.ChainMembers)

	if normalized.Name == "" {
		normalized.Name = defaultNodeName(normalized.Protocol, normalized.Server)
	}
	if !isSupportedNodeProtocol(normalized.Protocol) {
		return nil, ErrUnsupportedProtocol
	}
	if normalized.Protocol == ProtocolChain {
		normalized.Server = ""
		normalized.Port = nil
		normalized.Username = ""
		normalized.Password = ""
		normalized.RawURI = ""
		if len(normalized.ChainMembers) < 2 {
			return nil, ErrInvalidChain
		}
		return &normalized, nil
	}
	if normalized.Server == "" {
		return nil, ErrUnsupportedURI
	}
	if normalized.Port == nil || *normalized.Port == 0 {
		return nil, ErrInvalidPort
	}
	return &normalized, nil
}

func normalizeNodeChainIDs(ctx context.Context, tx model.DBTx, nodeID string, req *NodeUpsertRequest) error {
	if req == nil {
		return nil
	}
	if req.Protocol != ProtocolChain {
		req.ChainNodeIDs = nil
		req.ChainMembers = nil
		return nil
	}

	tx = model.GetTx(tx).WithContext(ctx)
	req.ChainMembers = normalizeChainMembers(req.ChainMembers)
	if len(req.ChainMembers) == 0 {
		req.ChainMembers = chainMembersFromNodeIDs(req.ChainNodeIDs)
	}
	req.ChainNodeIDs = chainNodeIDsFromMembers(req.ChainMembers)
	if len(req.ChainMembers) < 2 {
		return ErrInvalidChain
	}
	if nodeID != "" && containsString(req.ChainNodeIDs, nodeID) {
		return ErrInvalidChain
	}
	nodes, err := findNodesByIDs(ctx, tx, req.ChainNodeIDs)
	if err != nil {
		return err
	}
	if len(nodes) != len(req.ChainNodeIDs) {
		return ErrInvalidChain
	}
	for _, child := range nodes {
		if child.ID != nodeID && normalizeProtocol(child.Protocol) == ProtocolChain {
			return ErrInvalidChain
		}
	}
	groupIDs := chainGroupIDsFromMembers(req.ChainMembers)
	groups, err := findGroupsByIDs(ctx, tx, groupIDs)
	if err != nil {
		return err
	}
	if len(groups) != len(groupIDs) {
		return ErrInvalidChain
	}
	if len(groupIDs) > 0 {
		var allGroups []*tables.ProxyGroupTable
		if err := tx.Find(&allGroups).Error; err != nil {
			return err
		}
		if hasChainGroupCycle(allGroups, groupIDs) {
			return ErrInvalidChain
		}
	}
	return nil
}

func hasChainGroupCycle(groups []*tables.ProxyGroupTable, seedGroupIDs []string) bool {
	seedGroupIDs = uniqueNonEmpty(seedGroupIDs)
	if len(seedGroupIDs) == 0 {
		return false
	}

	byID := make(map[string]*tables.ProxyGroupTable, len(groups))
	for _, group := range groups {
		if group != nil && strings.TrimSpace(group.ID) != "" {
			byID[group.ID] = group
		}
	}

	visited := map[string]bool{}
	visiting := map[string]bool{}
	var visit func(string) bool
	visit = func(groupID string) bool {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			return false
		}
		if visiting[groupID] {
			return true
		}
		if visited[groupID] {
			return false
		}
		group := byID[groupID]
		if group == nil {
			return false
		}

		visiting[groupID] = true
		for _, childGroupID := range decodeStringSlice(group.GroupIDsJSON) {
			if visit(childGroupID) {
				return true
			}
		}
		delete(visiting, groupID)
		visited[groupID] = true
		return false
	}

	for _, groupID := range seedGroupIDs {
		if visit(groupID) {
			return true
		}
	}
	return false
}

func ensureNodeNotReferencedByChains(ctx context.Context, tx model.DBTx, nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var nodes []*tables.ProxyNodeTable
	if err := tx.Find(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		if node == nil || node.ID == nodeID {
			continue
		}
		if containsString(chainNodeIDsFromMembers(chainMembersForNode(node)), nodeID) {
			return ErrInvalidChain
		}
	}
	return nil
}

func ensureGroupNotReferencedByChains(ctx context.Context, tx model.DBTx, groupIDs []string) error {
	groupIDs = uniqueNonEmpty(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var groups []*tables.ProxyGroupTable
	if err := tx.Find(&groups).Error; err != nil {
		return err
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

	var nodes []*tables.ProxyNodeTable
	if err := tx.Find(&nodes).Error; err != nil {
		return err
	}
	for _, node := range nodes {
		if node == nil || normalizeProtocol(node.Protocol) != ProtocolChain {
			continue
		}
		if stringSlicesIntersect(chainGroupIDsFromMembers(chainMembersForNode(node)), expandedGroupIDs) {
			return ErrInvalidChain
		}
	}
	return nil
}

func normalizeMappingRequest(ctx context.Context, tx model.DBTx, mappingID string, req MappingUpsertRequest) (*MappingUpsertRequest, error) {
	normalized := req
	inheritedGroupStrategyOverrides := false
	if normalized.GroupStrategyOverrides == nil && mappingID != "" {
		var existing tables.PortMappingTable
		if err := tx.WithContext(ctx).First(&existing, "id = ?", mappingID).Error; err == nil {
			normalized.GroupStrategyOverrides = decodeGroupStrategyOverrides(existing.GroupStrategyOverridesJSON)
			inheritedGroupStrategyOverrides = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	normalized.ListenAddress = strings.TrimSpace(normalized.ListenAddress)
	if normalized.ListenAddress == "" {
		normalized.ListenAddress = "127.0.0.1"
	}
	if _, err := netip.ParseAddr(normalized.ListenAddress); err != nil {
		return nil, ErrInvalidAddress
	}
	if normalized.ListenPort == 0 {
		return nil, ErrInvalidPort
	}

	normalized.OutboundProtocol = normalizeOutboundProtocol(normalized.OutboundProtocol)
	normalized.Username = strings.TrimSpace(normalized.Username)
	normalized.Password = strings.TrimSpace(normalized.Password)
	normalized.Strategy = normalizeStrategy(normalized.Strategy)
	normalized.Remark = strings.TrimSpace(normalized.Remark)

	nodes, err := findNodesByIDs(ctx, tx, normalized.NodeIDs)
	if err != nil {
		return nil, err
	}
	normalized.NodeIDs = make([]string, 0, len(nodes))
	for _, node := range nodes {
		normalized.NodeIDs = append(normalized.NodeIDs, node.ID)
	}
	activeNode := ""
	if normalized.ActiveNodeID != nil {
		activeNode = strings.TrimSpace(*normalized.ActiveNodeID)
	}
	if activeNode != "" && !containsString(normalized.NodeIDs, activeNode) {
		activeNode = ""
	}

	groups, err := findGroupsByIDs(ctx, tx, normalized.GroupIDs)
	if err != nil {
		return nil, err
	}
	normalized.GroupIDs = make([]string, 0, len(groups))
	for _, group := range groups {
		normalized.GroupIDs = append(normalized.GroupIDs, group.ID)
	}
	if inheritedGroupStrategyOverrides {
		normalized.GroupStrategyOverrides = normalizeGroupStrategyOverrides(normalized.GroupStrategyOverrides, normalized.GroupIDs)
	} else {
		groupStrategyOverrides, err := normalizeMappingGroupStrategyOverrides(
			normalized.GroupStrategyOverrides,
			normalized.GroupIDs,
		)
		if err != nil {
			return nil, err
		}
		normalized.GroupStrategyOverrides = groupStrategyOverrides
	}
	activeGroup := ""
	if normalized.ActiveGroupID != nil {
		activeGroup = strings.TrimSpace(*normalized.ActiveGroupID)
	}
	if activeGroup != "" && !containsString(normalized.GroupIDs, activeGroup) {
		activeGroup = ""
	}

	if normalized.Strategy != StrategyManual {
		activeNode = ""
		activeGroup = ""
	} else {
		switch {
		case activeGroup != "":
			activeNode = ""
		case activeNode != "":
		case len(normalized.GroupIDs) > 0:
			activeGroup = normalized.GroupIDs[0]
		case len(normalized.NodeIDs) > 0:
			activeNode = normalized.NodeIDs[0]
		}
	}

	normalized.ActiveNodeID = stringPtrOrNil(activeNode)
	normalized.ActiveGroupID = stringPtrOrNil(activeGroup)

	if err := ensureListenPortAvailable(ctx, tx, mappingID, normalized.ListenPort); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func ensureListenPortAvailable(ctx context.Context, tx model.DBTx, mappingID string, listenPort uint16) error {
	var existing tables.PortMappingTable
	query := tx.WithContext(ctx).Where("listen_port = ?", listenPort)
	if mappingID != "" {
		query = query.Where("id <> ?", mappingID)
	}
	if err := query.First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return ErrListenPortTaken
}

func normalizeImportURIs(req NodeImportRequest) []string {
	values := make([]string, 0, len(req.URIs))
	values = append(values, req.URIs...)
	if raw := strings.TrimSpace(req.Raw); raw != "" {
		values = append(values, raw)
	}

	expanded := make([]string, 0, len(values))
	for _, value := range uniqueNonEmpty(values) {
		expanded = append(expanded, expandImportValue(value)...)
	}
	return uniqueNonEmpty(expanded)
}

func normalizeImportURIsWithFetch(ctx context.Context, req NodeImportRequest) ([]string, []NodeImportFailure) {
	values := make([]string, 0, len(req.URIs)+1)
	values = append(values, req.URIs...)
	if raw := strings.TrimSpace(req.Raw); raw != "" {
		values = append(values, raw)
	}

	expanded := make([]string, 0, len(values))
	failures := make([]NodeImportFailure, 0)
	for _, value := range uniqueNonEmpty(values) {
		if isLikelySubscriptionURL(value) {
			raw, err := fetchSubscription(ctx, value)
			if err != nil {
				failures = append(failures, NodeImportFailure{URI: value, Message: err.Error()})
				continue
			}
			expanded = append(expanded, expandImportValue(raw)...)
			continue
		}
		expanded = append(expanded, expandImportValue(value)...)
	}
	return uniqueNonEmpty(expanded), failures
}

func normalizeProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(protocol, ":")))
	switch protocol {
	case "socks", "socks5":
		return ProtocolSOCKS5
	case "ss", "shadowsocks":
		return ProtocolShadowsocks
	case "hy2", "hysteria2":
		return ProtocolHysteria2
	case "https":
		return ProtocolHTTP
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan, ProtocolHTTP,
		ProtocolHysteria, ProtocolTUIC, ProtocolSSH, ProtocolChain:
		return protocol
	default:
		return ProtocolUnknown
	}
}

func normalizeOutboundProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "socks", "socks5":
		return OutboundProtocolSOCKS
	case OutboundProtocolHTTP:
		return OutboundProtocolHTTP
	default:
		return OutboundProtocolMixed
	}
}

func normalizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case StrategyFailover:
		return StrategyFailover
	case StrategyLoadBalance:
		return StrategyLoadBalance
	case StrategyManual:
		return StrategyManual
	case StrategyLeastLatency:
		return StrategyLeastLatency
	default:
		return StrategyLeastLatency
	}
}

func normalizeGroupStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case GroupStrategyURLTest, StrategyFailover:
		return GroupStrategyURLTest
	case GroupStrategyLoadBalance:
		return GroupStrategyLoadBalance
	case GroupStrategyLeastLatency:
		return GroupStrategyLeastLatency
	default:
		return GroupStrategySelector
	}
}

func normalizeGroupStrategyOverride(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "", GroupStrategyOverrideInherit:
		return GroupStrategyOverrideInherit
	case GroupStrategyOverrideLoadBalance:
		return GroupStrategyOverrideLoadBalance
	case GroupStrategyOverrideLeastLatency:
		return GroupStrategyOverrideLeastLatency
	default:
		return ""
	}
}

func normalizeMappingGroupStrategyOverrides(values map[string]string, groupIDs []string) (map[string]string, error) {
	result := map[string]string{}
	if len(values) == 0 {
		return result, nil
	}
	allowed := stringSet(groupIDs)
	for groupID, strategy := range values {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		if _, ok := allowed[groupID]; !ok {
			return nil, ErrInvalidMapping
		}
		normalized := normalizeGroupStrategyOverride(strategy)
		if normalized == "" {
			return nil, ErrInvalidMapping
		}
		if normalized == GroupStrategyOverrideInherit {
			continue
		}
		result[groupID] = normalized
	}
	return result, nil
}

func normalizeGroupType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GroupTypeSubscription:
		return GroupTypeSubscription
	default:
		return GroupTypeManual
	}
}

func isSupportedNodeProtocol(protocol string) bool {
	switch protocol {
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan, ProtocolSOCKS5, ProtocolHTTP,
		ProtocolShadowsocks, ProtocolHysteria, ProtocolHysteria2, ProtocolTUIC, ProtocolSSH,
		ProtocolChain:
		return true
	default:
		return false
	}
}

func cleanTags(tags []string, protocol string) []string {
	values := uniqueNonEmpty(tags)
	if protocol != "" && protocol != ProtocolUnknown && !containsString(values, protocol) {
		values = append([]string{protocol}, values...)
	}
	return values
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func removeString(values []string, target string) []string {
	next := values[:0]
	for _, value := range values {
		if value != target {
			next = append(next, value)
		}
	}
	return next
}

func removeStrings(values []string, targets []string) []string {
	targetSet := stringSet(targets)
	if len(targetSet) == 0 {
		return uniqueNonEmpty(values)
	}
	next := values[:0]
	for _, value := range values {
		if _, ok := targetSet[value]; ok {
			continue
		}
		next = append(next, value)
	}
	return uniqueNonEmpty(next)
}

func stringSet(values []string) map[string]struct{} {
	values = uniqueNonEmpty(values)
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func defaultNodeName(protocol, server string) string {
	if server == "" {
		return "未命名节点"
	}
	if protocol == "" || protocol == ProtocolUnknown {
		return server
	}
	return strings.ToUpper(protocol) + " " + server
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "unique violation") ||
		strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "duplicate key")
}
