package proxy

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SettingsBackupKind          = "proxyhub.proxy-settings"
	SettingsBackupSchemaVersion = 1
)

type SettingsBackupDataDTO struct {
	Nodes         []*ProxyNodeDTO         `json:"nodes"`
	Groups        []*ProxyGroupDTO        `json:"groups"`
	Subscriptions []*ProxySubscriptionDTO `json:"subscriptions"`
	Mappings      []*PortMappingDTO       `json:"mappings"`
}

type SettingsBackupDTO struct {
	Kind          string                `json:"kind"`
	SchemaVersion int                   `json:"schemaVersion"`
	ExportedAt    time.Time             `json:"exportedAt"`
	Data          SettingsBackupDataDTO `json:"data"`
}

type SettingsImportResultDTO struct {
	Message              string            `json:"message"`
	Nodes                int               `json:"nodes"`
	Groups               int               `json:"groups"`
	Subscriptions        int               `json:"subscriptions"`
	Mappings             int               `json:"mappings"`
	RuntimeReloadWarning string            `json:"runtimeReloadWarning,omitempty"`
	State                *StateSnapshotDTO `json:"state"`
}

type settingsBackupRows struct {
	nodes         []*tables.ProxyNodeTable
	groups        []*tables.ProxyGroupTable
	subscriptions []*tables.ProxySubscriptionTable
	mappings      []*tables.PortMappingTable
}

func SettingsExport(ctx context.Context, tx model.DBTx) (*SettingsBackupDTO, error) {
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

	return &SettingsBackupDTO{
		Kind:          SettingsBackupKind,
		SchemaVersion: SettingsBackupSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		Data: SettingsBackupDataDTO{
			Nodes:         ToNodeDTOsWithGroups(nodes, groups),
			Groups:        ToGroupDTOs(groups),
			Subscriptions: ToSubscriptionDTOs(subscriptions),
			Mappings:      ToMappingDTOs(mappings),
		},
	}, nil
}

func SettingsImport(ctx context.Context, backup SettingsBackupDTO) (*SettingsImportResultDTO, error) {
	rows, err := validateSettingsBackup(backup)
	if err != nil {
		return nil, err
	}

	discardNodeHealthBatcher()
	if err := model.Transaction(ctx, func(tx model.DBTx) error {
		return replaceSettingsRows(ctx, tx, rows)
	}); err != nil {
		return nil, err
	}
	discardNodeHealthBatcher()

	result := &SettingsImportResultDTO{
		Message:       "代理设置已导入",
		Nodes:         len(rows.nodes),
		Groups:        len(rows.groups),
		Subscriptions: len(rows.subscriptions),
		Mappings:      len(rows.mappings),
	}

	status, reloadErr := RuntimeReload(context.Background())
	if reloadErr != nil {
		result.RuntimeReloadWarning = reloadErr.Error()
	} else if status.Running {
		result.Message = "代理设置已导入并重载运行时"
	}

	snapshot, err := StateSnapshot(ctx, nil)
	if err != nil {
		return nil, err
	}
	result.State = snapshot
	return result, nil
}

func validateSettingsBackup(backup SettingsBackupDTO) (*settingsBackupRows, error) {
	if backup.Kind != SettingsBackupKind || backup.SchemaVersion != SettingsBackupSchemaVersion {
		return nil, invalidSettingsBackup("unsupported settings backup")
	}

	rows := &settingsBackupRows{
		subscriptions: make([]*tables.ProxySubscriptionTable, 0, len(backup.Data.Subscriptions)),
		groups:        make([]*tables.ProxyGroupTable, 0, len(backup.Data.Groups)),
		nodes:         make([]*tables.ProxyNodeTable, 0, len(backup.Data.Nodes)),
		mappings:      make([]*tables.PortMappingTable, 0, len(backup.Data.Mappings)),
	}

	subscriptionIDs := make(map[string]struct{}, len(backup.Data.Subscriptions))
	for _, dto := range backup.Data.Subscriptions {
		row, err := subscriptionDTOToTable(dto)
		if err != nil {
			return nil, err
		}
		if err := rememberID(subscriptionIDs, row.ID, "subscription"); err != nil {
			return nil, err
		}
		rows.subscriptions = append(rows.subscriptions, row)
	}

	groupIDs := make(map[string]struct{}, len(backup.Data.Groups))
	for _, dto := range backup.Data.Groups {
		row, err := groupDTOToTable(dto, subscriptionIDs)
		if err != nil {
			return nil, err
		}
		if err := rememberID(groupIDs, row.ID, "group"); err != nil {
			return nil, err
		}
		rows.groups = append(rows.groups, row)
	}

	nodeIDs := make(map[string]struct{}, len(backup.Data.Nodes))
	nodeByID := make(map[string]*tables.ProxyNodeTable, len(backup.Data.Nodes))
	for _, dto := range backup.Data.Nodes {
		row, err := nodeDTOToTable(dto, subscriptionIDs, groupIDs)
		if err != nil {
			return nil, err
		}
		if err := rememberID(nodeIDs, row.ID, "node"); err != nil {
			return nil, err
		}
		nodeByID[row.ID] = row
		rows.nodes = append(rows.nodes, row)
	}
	for _, node := range rows.nodes {
		if err := validateNodeReferences(node, nodeByID, groupIDs); err != nil {
			return nil, err
		}
	}

	listenPorts := make(map[uint16]struct{}, len(backup.Data.Mappings))
	for _, dto := range backup.Data.Mappings {
		row, err := mappingDTOToTable(dto, nodeIDs, groupIDs)
		if err != nil {
			return nil, err
		}
		if row.ID == "" {
			return nil, invalidSettingsBackup("mapping id is required")
		}
		if _, ok := listenPorts[row.ListenPort]; ok {
			return nil, invalidSettingsBackup("duplicate mapping listen port")
		}
		listenPorts[row.ListenPort] = struct{}{}
		rows.mappings = append(rows.mappings, row)
	}

	for _, group := range rows.groups {
		if err := validateGroupReferences(group, nodeIDs, groupIDs); err != nil {
			return nil, err
		}
	}
	if err := validateGroupGraphForChainMembers(rows.groups, rows.nodes); err != nil {
		return nil, err
	}

	for _, subscription := range rows.subscriptions {
		if subscription.GroupID != "" {
			if _, ok := groupIDs[subscription.GroupID]; !ok {
				return nil, invalidSettingsBackup("subscription references missing group")
			}
		}
	}

	return rows, nil
}

func replaceSettingsRows(ctx context.Context, tx model.DBTx, rows *settingsBackupRows) error {
	tx = model.GetTx(tx).WithContext(ctx)
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&tables.PortMappingTable{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&tables.ProxyNodeHealthTable{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&tables.ProxyNodeHealthHistoryTable{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&tables.ProxyNodeTable{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&tables.ProxyGroupTable{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&tables.ProxySubscriptionTable{}).Error; err != nil {
		return err
	}

	if len(rows.subscriptions) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows.subscriptions).Error; err != nil {
			return err
		}
	}
	if len(rows.groups) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows.groups).Error; err != nil {
			return err
		}
	}
	if len(rows.nodes) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows.nodes).Error; err != nil {
			return err
		}
	}
	if len(rows.mappings) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows.mappings).Error; err != nil {
			return err
		}
	}
	return nil
}

func subscriptionDTOToTable(dto *ProxySubscriptionDTO) (*tables.ProxySubscriptionTable, error) {
	if dto == nil {
		return nil, invalidSettingsBackup("subscription is empty")
	}
	row := &tables.ProxySubscriptionTable{
		Name:           strings.TrimSpace(dto.Name),
		URL:            strings.TrimSpace(dto.URL),
		GroupID:        strings.TrimSpace(dto.GroupID),
		Remark:         strings.TrimSpace(dto.Remark),
		LastSyncedAt:   dto.LastSyncedAt,
		LastSyncStatus: strings.TrimSpace(dto.LastSyncStatus),
		LastSyncError:  strings.TrimSpace(dto.LastSyncError),
	}
	row.ID = strings.TrimSpace(dto.ID)
	row.CreatedAt = dto.CreatedAt
	row.UpdatedAt = dto.UpdatedAt
	if row.ID == "" || row.Name == "" || row.URL == "" {
		return nil, invalidSettingsBackup("subscription id, name, and url are required")
	}
	normalizeBackupTimestamps(&row.CreatedAt, &row.UpdatedAt)
	return row, nil
}

func groupDTOToTable(dto *ProxyGroupDTO, subscriptionIDs map[string]struct{}) (*tables.ProxyGroupTable, error) {
	if dto == nil {
		return nil, invalidSettingsBackup("group is empty")
	}
	subscriptionID := strings.TrimSpace(dto.SubscriptionID)
	if subscriptionID != "" {
		if _, ok := subscriptionIDs[subscriptionID]; !ok {
			return nil, invalidSettingsBackup("group references missing subscription")
		}
	}
	row := &tables.ProxyGroupTable{
		Name:            strings.TrimSpace(dto.Name),
		Type:            normalizeGroupType(dto.Type),
		Strategy:        normalizeGroupStrategy(dto.Strategy),
		SubscriptionID:  subscriptionID,
		SourceKey:       strings.TrimSpace(dto.SourceKey),
		NodeIDsJSON:     encodeStringSlice(uniqueNonEmpty(dto.NodeIDs)),
		GroupIDsJSON:    encodeStringSlice(uniqueNonEmpty(dto.GroupIDs)),
		BuiltinTagsJSON: encodeStringSlice(uniqueNonEmpty(dto.BuiltinTags)),
		IncludesAll:     dto.IncludesAll,
		Filter:          strings.TrimSpace(dto.Filter),
		Remark:          strings.TrimSpace(dto.Remark),
	}
	row.ID = strings.TrimSpace(dto.ID)
	row.CreatedAt = dto.CreatedAt
	row.UpdatedAt = dto.UpdatedAt
	if row.ID == "" || row.Name == "" {
		return nil, invalidSettingsBackup("group id and name are required")
	}
	normalizeBackupTimestamps(&row.CreatedAt, &row.UpdatedAt)
	return row, nil
}

func nodeDTOToTable(dto *ProxyNodeDTO, subscriptionIDs, groupIDs map[string]struct{}) (*tables.ProxyNodeTable, error) {
	if dto == nil {
		return nil, invalidSettingsBackup("node is empty")
	}
	subscriptionID := strings.TrimSpace(dto.SubscriptionID)
	if subscriptionID != "" {
		if _, ok := subscriptionIDs[subscriptionID]; !ok {
			return nil, invalidSettingsBackup("node references missing subscription")
		}
	}
	groupID := strings.TrimSpace(dto.GroupID)
	if groupID != "" {
		if _, ok := groupIDs[groupID]; !ok {
			return nil, invalidSettingsBackup("node references missing group")
		}
	}
	for _, groupID := range uniqueNonEmpty(dto.GroupIDs) {
		if _, ok := groupIDs[groupID]; !ok {
			return nil, invalidSettingsBackup("node references missing group")
		}
	}

	protocol := normalizeProtocol(dto.Protocol)
	if !isSupportedNodeProtocol(protocol) {
		return nil, invalidSettingsBackup("node protocol is unsupported")
	}
	if protocol != ProtocolChain && (strings.TrimSpace(dto.Server) == "" || dto.Port == nil || *dto.Port == 0) {
		return nil, invalidSettingsBackup("node server and port are required")
	}
	chainMembers := normalizeChainMembers(dto.ChainMembers)
	if len(chainMembers) == 0 {
		chainMembers = chainMembersFromNodeIDs(dto.ChainNodeIDs)
	}
	chainNodeIDs := chainNodeIDsFromMembers(chainMembers)
	if protocol == ProtocolChain && len(chainMembers) < 2 {
		return nil, invalidSettingsBackup("chain node requires at least two child nodes")
	}

	row := &tables.ProxyNodeTable{
		Name:             strings.TrimSpace(dto.Name),
		Protocol:         protocol,
		Server:           strings.TrimSpace(dto.Server),
		Port:             dto.Port,
		Username:         strings.TrimSpace(dto.Username),
		Password:         strings.TrimSpace(dto.Password),
		RawURI:           strings.TrimSpace(dto.RawURI),
		TagsJSON:         encodeStringSlice(uniqueNonEmpty(dto.Tags)),
		Remark:           strings.TrimSpace(dto.Remark),
		ChainNodeIDsJSON: encodeStringSlice(chainNodeIDs),
		ChainMembersJSON: encodeChainMembers(chainMembers),
		SubscriptionID:   subscriptionID,
		GroupID:          groupID,
		SourceKey:        strings.TrimSpace(dto.SourceKey),
	}
	row.ID = strings.TrimSpace(dto.ID)
	row.CreatedAt = dto.CreatedAt
	row.UpdatedAt = dto.UpdatedAt
	if row.ID == "" || row.Name == "" {
		return nil, invalidSettingsBackup("node id and name are required")
	}
	normalizeBackupTimestamps(&row.CreatedAt, &row.UpdatedAt)
	return row, nil
}

func mappingDTOToTable(dto *PortMappingDTO, nodeIDs, groupIDs map[string]struct{}) (*tables.PortMappingTable, error) {
	if dto == nil {
		return nil, invalidSettingsBackup("mapping is empty")
	}
	if dto.ListenPort == 0 {
		return nil, invalidSettingsBackup("mapping listen port is required")
	}

	normalizedNodeIDs := uniqueNonEmpty(dto.NodeIDs)
	for _, nodeID := range normalizedNodeIDs {
		if _, ok := nodeIDs[nodeID]; !ok {
			return nil, invalidSettingsBackup("mapping references missing node")
		}
	}
	activeNodeID := valueOrEmpty(dto.ActiveNodeID)
	if activeNodeID != "" && !containsString(normalizedNodeIDs, activeNodeID) {
		return nil, invalidSettingsBackup("mapping active node is not in node list")
	}

	normalizedGroupIDs := uniqueNonEmpty(dto.GroupIDs)
	for _, groupID := range normalizedGroupIDs {
		if _, ok := groupIDs[groupID]; !ok {
			return nil, invalidSettingsBackup("mapping references missing group")
		}
	}
	groupStrategyOverrides, err := normalizeMappingGroupStrategyOverrides(dto.GroupStrategyOverrides, normalizedGroupIDs)
	if err != nil {
		return nil, invalidSettingsBackup("mapping group strategy override is invalid")
	}
	activeGroupID := valueOrEmpty(dto.ActiveGroupID)
	if activeGroupID != "" && !containsString(normalizedGroupIDs, activeGroupID) {
		return nil, invalidSettingsBackup("mapping active group is not in group list")
	}

	row := &tables.PortMappingTable{
		Enabled:                    dto.Enabled,
		ListenAddress:              strings.TrimSpace(dto.ListenAddress),
		ListenPort:                 dto.ListenPort,
		Order:                      dto.Order,
		OutboundProtocol:           normalizeOutboundProtocol(dto.OutboundProtocol),
		Username:                   strings.TrimSpace(dto.Username),
		Password:                   strings.TrimSpace(dto.Password),
		Strategy:                   normalizeStrategy(dto.Strategy),
		NodeIDsJSON:                encodeStringSlice(normalizedNodeIDs),
		ActiveNodeID:               activeNodeID,
		GroupIDsJSON:               encodeStringSlice(normalizedGroupIDs),
		GroupStrategyOverridesJSON: encodeGroupStrategyOverrides(groupStrategyOverrides),
		ActiveGroupID:              activeGroupID,
		Remark:                     strings.TrimSpace(dto.Remark),
	}
	row.ID = strings.TrimSpace(dto.ID)
	row.CreatedAt = dto.CreatedAt
	row.UpdatedAt = dto.UpdatedAt
	if row.ListenAddress == "" {
		row.ListenAddress = "127.0.0.1"
	}
	if _, err := netip.ParseAddr(row.ListenAddress); err != nil {
		return nil, invalidSettingsBackup("mapping listen address is invalid")
	}
	if row.Order == 0 {
		row.Order = 1
	}
	normalizeBackupTimestamps(&row.CreatedAt, &row.UpdatedAt)
	return row, nil
}

func validateNodeReferences(node *tables.ProxyNodeTable, nodeByID map[string]*tables.ProxyNodeTable, groupIDs map[string]struct{}) error {
	if node.Protocol != ProtocolChain {
		return nil
	}
	for _, member := range chainMembersForNode(node) {
		switch member.Type {
		case ChainMemberTypeNode:
			if member.ID == node.ID {
				return invalidSettingsBackup("chain node cannot reference itself")
			}
			child := nodeByID[member.ID]
			if child == nil {
				return invalidSettingsBackup("chain node references missing node")
			}
			if child.Protocol == ProtocolChain {
				return invalidSettingsBackup("chain node cannot reference another chain node")
			}
		case ChainMemberTypeGroup:
			if _, ok := groupIDs[member.ID]; !ok {
				return invalidSettingsBackup("chain node references missing group")
			}
		default:
			return invalidSettingsBackup("chain node has invalid member")
		}
	}
	return nil
}

func validateGroupGraphForChainMembers(groups []*tables.ProxyGroupTable, nodes []*tables.ProxyNodeTable) error {
	for _, node := range nodes {
		if node == nil || node.Protocol != ProtocolChain {
			continue
		}
		if hasChainGroupCycle(groups, chainGroupIDsFromMembers(chainMembersForNode(node))) {
			return invalidSettingsBackup("chain node references cyclic group")
		}
	}
	return nil
}

func validateGroupReferences(group *tables.ProxyGroupTable, nodeIDs, groupIDs map[string]struct{}) error {
	for _, nodeID := range decodeStringSlice(group.NodeIDsJSON) {
		if _, ok := nodeIDs[nodeID]; !ok {
			return invalidSettingsBackup("group references missing node")
		}
	}
	for _, nestedGroupID := range decodeStringSlice(group.GroupIDsJSON) {
		if nestedGroupID == group.ID {
			return invalidSettingsBackup("group cannot reference itself")
		}
		if _, ok := groupIDs[nestedGroupID]; !ok {
			return invalidSettingsBackup("group references missing group")
		}
	}
	return nil
}

func rememberID(seen map[string]struct{}, id, label string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return invalidSettingsBackup(label + " id is required")
	}
	if _, ok := seen[id]; ok {
		return invalidSettingsBackup("duplicate " + label + " id")
	}
	seen[id] = struct{}{}
	return nil
}

func normalizeBackupTimestamps(createdAt *time.Time, updatedAt *time.Time) {
	now := time.Now()
	if createdAt.IsZero() {
		*createdAt = now
	}
	if updatedAt.IsZero() {
		*updatedAt = *createdAt
	}
}

func invalidSettingsBackup(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidSettingsBackup, message)
}
