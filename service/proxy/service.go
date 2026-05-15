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
	groupID := strings.TrimSpace(req.GroupID)
	if groupID != "" {
		if _, err := GroupGet(ctx, tx, groupID); err != nil {
			return nil, err
		}
	}

	node := &tables.ProxyNodeTable{
		Name:           normalized.Name,
		Protocol:       normalized.Protocol,
		Server:         normalized.Server,
		Port:           normalized.Port,
		Username:       normalized.Username,
		Password:       normalized.Password,
		RawURI:         normalized.RawURI,
		TagsJSON:       encodeStringSlice(normalized.Tags),
		Remark:         normalized.Remark,
		SubscriptionID: strings.TrimSpace(req.SubscriptionID),
		GroupID:        groupID,
		SourceKey:      strings.TrimSpace(req.SourceKey),
	}
	if err := tx.Create(node).Error; err != nil {
		return nil, err
	}
	if groupID != "" {
		if err := addNodesToGroupMembership(ctx, tx, groupID, []string{node.ID}); err != nil {
			return nil, err
		}
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
	groupID := strings.TrimSpace(req.GroupID)
	if groupID != "" {
		if _, err := GroupGet(ctx, tx, groupID); err != nil {
			return nil, err
		}
	}

	previousGroupID := strings.TrimSpace(node.GroupID)
	if previousGroupID != "" && previousGroupID != groupID {
		if err := removeNodesFromGroupMembership(ctx, tx, previousGroupID, []string{node.ID}); err != nil {
			return nil, err
		}
	}
	if groupID != "" {
		if err := addNodesToGroupMembership(ctx, tx, groupID, []string{node.ID}); err != nil {
			return nil, err
		}
	}

	if err := tx.Model(&node).Updates(map[string]any{
		"name":       normalized.Name,
		"protocol":   normalized.Protocol,
		"server":     normalized.Server,
		"port":       normalized.Port,
		"username":   normalized.Username,
		"password":   normalized.Password,
		"raw_uri":    normalized.RawURI,
		"tags_json":  encodeStringSlice(normalized.Tags),
		"remark":     normalized.Remark,
		"group_id":   groupID,
		"updated_at": time.Now(),
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

	var mappings []*tables.PortMappingTable
	if err := tx.Find(&mappings).Error; err != nil {
		return err
	}
	for _, mapping := range mappings {
		nodeIDs := removeString(decodeStringSlice(mapping.NodeIDsJSON), id)
		active := mapping.ActiveNodeID
		if active == id {
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

	return tx.Delete(&node).Error
}

func NodeImport(ctx context.Context, tx model.DBTx, req NodeImportRequest) (*NodeImportResult, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	req.GroupID = strings.TrimSpace(req.GroupID)
	if req.GroupID != "" {
		if _, err := GroupGet(ctx, tx, req.GroupID); err != nil {
			return nil, err
		}
	}

	uris, fetchFailures := normalizeImportURIsWithFetch(ctx, req)
	result := &NodeImportResult{Total: len(uris), Failures: fetchFailures, Skipped: len(fetchFailures)}
	for _, rawURI := range uris {
		parsed, err := ParseNodeURI(rawURI)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: rawURI, Message: err.Error()})
			continue
		}
		parsed.GroupID = req.GroupID
		node, err := NodeCreate(ctx, tx, *parsed)
		if err != nil {
			result.Failures = append(result.Failures, NodeImportFailure{URI: rawURI, Message: err.Error()})
			continue
		}
		result.Items = append(result.Items, ToNodeDTO(node))
	}
	result.Imported = len(result.Items)
	result.Failed = len(result.Failures)
	return result, nil
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
		Enabled:          normalized.Enabled,
		ListenAddress:    normalized.ListenAddress,
		ListenPort:       normalized.ListenPort,
		Order:            order,
		OutboundProtocol: normalized.OutboundProtocol,
		Username:         normalized.Username,
		Password:         normalized.Password,
		Strategy:         normalized.Strategy,
		NodeIDsJSON:      encodeStringSlice(normalized.NodeIDs),
		ActiveNodeID:     valueOrEmpty(normalized.ActiveNodeID),
		GroupIDsJSON:     encodeStringSlice(normalized.GroupIDs),
		ActiveGroupID:    valueOrEmpty(normalized.ActiveGroupID),
		Remark:           normalized.Remark,
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
		"enabled":           normalized.Enabled,
		"listen_address":    normalized.ListenAddress,
		"listen_port":       normalized.ListenPort,
		"outbound_protocol": normalized.OutboundProtocol,
		"username":          normalized.Username,
		"password":          normalized.Password,
		"strategy":          normalized.Strategy,
		"node_ids_json":     encodeStringSlice(normalized.NodeIDs),
		"active_node_id":    valueOrEmpty(normalized.ActiveNodeID),
		"group_ids_json":    encodeStringSlice(normalized.GroupIDs),
		"active_group_id":   valueOrEmpty(normalized.ActiveGroupID),
		"remark":            normalized.Remark,
		"updated_at":        time.Now(),
	}).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrListenPortTaken
		}
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
	return tx.Delete(&mapping).Error
}

func StateSnapshot(ctx context.Context, tx model.DBTx) (*StateSnapshotDTO, error) {
	nodes, err := NodeList(ctx, tx)
	if err != nil {
		return nil, err
	}
	groups, err := GroupList(ctx, tx)
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
	return &StateSnapshotDTO{
		Nodes:         ToNodeDTOs(nodes),
		Groups:        ToGroupDTOs(groups),
		Subscriptions: ToSubscriptionDTOs(subscriptions),
		Mappings:      ToMappingDTOs(mappings),
		Runtime:       RuntimeStatusGet(),
		LastSavedAt:   time.Now(),
	}, nil
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

	if normalized.Name == "" {
		normalized.Name = defaultNodeName(normalized.Protocol, normalized.Server)
	}
	if !isSupportedNodeProtocol(normalized.Protocol) {
		return nil, ErrUnsupportedProtocol
	}
	if normalized.Server == "" {
		return nil, ErrUnsupportedURI
	}
	if normalized.Port == nil || *normalized.Port == 0 {
		return nil, ErrInvalidPort
	}
	return &normalized, nil
}

func normalizeMappingRequest(ctx context.Context, tx model.DBTx, mappingID string, req MappingUpsertRequest) (*MappingUpsertRequest, error) {
	normalized := req
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
	active := ""
	if normalized.ActiveNodeID != nil {
		active = strings.TrimSpace(*normalized.ActiveNodeID)
	}
	if active != "" && !containsString(normalized.NodeIDs, active) {
		active = ""
	}
	if active == "" && len(normalized.NodeIDs) > 0 {
		active = normalized.NodeIDs[0]
	}
	normalized.ActiveNodeID = stringPtrOrNil(active)

	groups, err := findGroupsByIDs(ctx, tx, normalized.GroupIDs)
	if err != nil {
		return nil, err
	}
	normalized.GroupIDs = make([]string, 0, len(groups))
	for _, group := range groups {
		normalized.GroupIDs = append(normalized.GroupIDs, group.ID)
	}
	activeGroup := ""
	if normalized.ActiveGroupID != nil {
		activeGroup = strings.TrimSpace(*normalized.ActiveGroupID)
	}
	if activeGroup != "" && !containsString(normalized.GroupIDs, activeGroup) {
		activeGroup = ""
	}
	if activeGroup == "" && len(normalized.GroupIDs) > 0 {
		activeGroup = normalized.GroupIDs[0]
	}
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
	case "https":
		return ProtocolHTTP
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan, ProtocolHTTP:
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
	default:
		return StrategyManual
	}
}

func normalizeGroupStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case GroupStrategyURLTest, StrategyFailover, StrategyLoadBalance:
		return GroupStrategyURLTest
	default:
		return GroupStrategySelector
	}
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
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan, ProtocolSOCKS5, ProtocolHTTP:
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
