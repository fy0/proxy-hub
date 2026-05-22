package proxy

import (
	"encoding/json"
	"strings"
	"time"

	"proxy-hub/model/tables"
)

const (
	ProtocolVLESS       = "vless"
	ProtocolVMess       = "vmess"
	ProtocolTrojan      = "trojan"
	ProtocolSOCKS5      = "socks5"
	ProtocolHTTP        = "http"
	ProtocolShadowsocks = "shadowsocks"
	ProtocolHysteria    = "hysteria"
	ProtocolHysteria2   = "hysteria2"
	ProtocolTUIC        = "tuic"
	ProtocolSSH         = "ssh"
	ProtocolChain       = "chain"
	ProtocolUnknown     = "unknown"

	OutboundProtocolMixed = "mixed"
	OutboundProtocolSOCKS = "socks5"
	OutboundProtocolHTTP  = "http"

	StrategyFailover     = "failover"
	StrategyLoadBalance  = "load-balance"
	StrategyManual       = "manual"
	StrategyLeastLatency = "least-latency"

	GroupTypeManual       = "manual"
	GroupTypeSubscription = "subscription"

	GroupStrategySelector     = "selector"
	GroupStrategyURLTest      = "url-test"
	GroupStrategyLoadBalance  = "load-balance"
	GroupStrategyLeastLatency = "least-latency"

	ChainMemberTypeNode  = "node"
	ChainMemberTypeGroup = "group"

	SubscriptionSyncStatusSuccess = "success"
	SubscriptionSyncStatusFailed  = "failed"
)

type ChainMemberDTO struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ProxyNodeDTO struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Protocol       string              `json:"protocol"`
	Server         string              `json:"server"`
	Port           *uint16             `json:"port"`
	Username       string              `json:"username"`
	Password       string              `json:"password"`
	RawURI         string              `json:"rawUri"`
	Tags           []string            `json:"tags"`
	Remark         string              `json:"remark"`
	ChainNodeIDs   []string            `json:"chainNodeIds"`
	ChainMembers   []ChainMemberDTO    `json:"chainMembers"`
	SubscriptionID string              `json:"subscriptionId"`
	GroupID        string              `json:"groupId"`
	GroupIDs       []string            `json:"groupIds"`
	SourceKey      string              `json:"sourceKey"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
	Health         *ProxyNodeHealthDTO `json:"health,omitempty"`
}

type ProxyNodeOptionDTO struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Protocol string   `json:"protocol"`
	Server   string   `json:"server"`
	Port     *uint16  `json:"port"`
	GroupIDs []string `json:"groupIds"`
}

type ProxyNodeHealthDTO struct {
	NodeID                  string     `json:"nodeId"`
	Available               bool       `json:"available"`
	FailureCount            int        `json:"failureCount"`
	SuccessCount            int64      `json:"successCount"`
	ConsecutiveFailureCount int        `json:"consecutiveFailureCount"`
	Blacklisted             bool       `json:"blacklisted"`
	BlacklistedUntil        *time.Time `json:"blacklistedUntil"`
	LastLatencyMs           int64      `json:"lastLatencyMs"`
	LastError               string     `json:"lastError"`
	LastCheckedAt           *time.Time `json:"lastCheckedAt"`
	LastSuccessAt           *time.Time `json:"lastSuccessAt"`
	LastFailureAt           *time.Time `json:"lastFailureAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

type ProxySubscriptionDTO struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	GroupID        string     `json:"groupId"`
	Remark         string     `json:"remark"`
	LastSyncedAt   *time.Time `json:"lastSyncedAt"`
	LastSyncStatus string     `json:"lastSyncStatus"`
	LastSyncError  string     `json:"lastSyncError"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ProxyGroupDTO struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Strategy       string    `json:"strategy"`
	SubscriptionID string    `json:"subscriptionId"`
	SourceKey      string    `json:"sourceKey"`
	NodeIDs        []string  `json:"nodeIds"`
	GroupIDs       []string  `json:"groupIds"`
	BuiltinTags    []string  `json:"builtinTags"`
	IncludesAll    bool      `json:"includesAll"`
	Filter         string    `json:"filter"`
	Remark         string    `json:"remark"`
	NodeCount      int       `json:"nodeCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PortMappingDTO struct {
	ID               string    `json:"id"`
	Enabled          bool      `json:"enabled"`
	ListenAddress    string    `json:"listenAddress"`
	ListenPort       uint16    `json:"listenPort"`
	Order            int64     `json:"order"`
	OutboundProtocol string    `json:"outboundProtocol"`
	Username         string    `json:"username"`
	Password         string    `json:"password"`
	Strategy         string    `json:"strategy"`
	NodeIDs          []string  `json:"nodeIds"`
	ActiveNodeID     *string   `json:"activeNodeId"`
	GroupIDs         []string  `json:"groupIds"`
	ActiveGroupID    *string   `json:"activeGroupId"`
	Remark           string    `json:"remark"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type StateSnapshotDTO struct {
	Nodes         []*ProxyNodeDTO         `json:"nodes"`
	Groups        []*ProxyGroupDTO        `json:"groups"`
	Subscriptions []*ProxySubscriptionDTO `json:"subscriptions"`
	Mappings      []*PortMappingDTO       `json:"mappings"`
	Runtime       RuntimeStatus           `json:"runtime"`
	LastSavedAt   time.Time               `json:"lastSavedAt"`
	NodeTotal     int64                   `json:"nodeTotal"`
	DefaultTotal  int64                   `json:"defaultTotal"`
}

type StateSnapshotOptions struct {
	IncludeNodes        bool
	IncludeGroupMembers bool
}

type NodeListRequest struct {
	Keyword      string
	NameOnly     bool
	GroupID      string
	DefaultOnly  bool
	PhysicalOnly bool
	IDs          []string
}

type NodeUpsertRequest struct {
	Name           string           `json:"name,omitempty" validate:"omitempty,max=100"`
	Protocol       string           `json:"protocol,omitempty" validate:"omitempty"`
	Server         string           `json:"server,omitempty" validate:"omitempty,max=255"`
	Port           *uint16          `json:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	Username       string           `json:"username,omitempty" validate:"omitempty,max=255"`
	Password       string           `json:"password,omitempty" validate:"omitempty,max=500"`
	RawURI         string           `json:"rawUri,omitempty" validate:"omitempty"`
	Tags           []string         `json:"tags,omitempty" validate:"omitempty"`
	Remark         string           `json:"remark,omitempty" validate:"omitempty,max=500"`
	ChainNodeIDs   []string         `json:"chainNodeIds,omitempty" validate:"omitempty"`
	ChainMembers   []ChainMemberDTO `json:"chainMembers,omitempty" validate:"omitempty"`
	SubscriptionID string           `json:"-"`
	GroupID        string           `json:"groupId,omitempty"`
	GroupIDs       []string         `json:"groupIds,omitempty"`
	SourceKey      string           `json:"-"`
}

type NodeImportRequest struct {
	Raw     string   `json:"raw,omitempty" doc:"多行分享链接文本"`
	URIs    []string `json:"uris,omitempty" doc:"分享链接列表"`
	GroupID string   `json:"groupId,omitempty" doc:"导入到指定节点组"`
}

type NodeImportFailure struct {
	URI     string `json:"uri"`
	Message string `json:"message"`
}

const (
	ImportPreviewTypeNode    = "node"
	ImportPreviewTypeGroup   = "group"
	ImportPreviewTypeBuiltin = "builtin"
	ImportPreviewTypeFailure = "failure"

	ImportPreviewActionImport = "import"
	ImportPreviewActionUpdate = "update"
	ImportPreviewActionSkip   = "skip"
	ImportPreviewActionFail   = "fail"

	ImportPreviewReasonImport               = "import"
	ImportPreviewReasonUpdate               = "update"
	ImportPreviewReasonRulesetPolicyGroup   = "ruleset-policy-group"
	ImportPreviewReasonGroupOnlyDirect      = "group-only-direct-ignored"
	ImportPreviewReasonUnsupportedProtocol  = "unsupported-protocol"
	ImportPreviewReasonInvalidURI           = "invalid-uri"
	ImportPreviewReasonFetchFailed          = "fetch-failed"
	ImportPreviewReasonManualGroupGenerated = "manual-group-generated"
)

type NodeImportPreviewItem struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type NodeImportResult struct {
	Items        []*ProxyNodeDTO         `json:"items"`
	Groups       []*ProxyGroupDTO        `json:"groups"`
	Failures     []NodeImportFailure     `json:"failures"`
	PreviewItems []NodeImportPreviewItem `json:"previewItems"`
	Total        int                     `json:"total"`
	Imported     int                     `json:"imported"`
	Failed       int                     `json:"failed"`
	Updated      int                     `json:"updated"`
	Deleted      int                     `json:"deleted"`
	Skipped      int                     `json:"skipped"`
}

type NodeHealthProbeAllResult struct {
	Items          []*tables.ProxyNodeHealthTable `json:"-"`
	Total          int                            `json:"total"`
	Queued         int                            `json:"queued"`
	Available      int                            `json:"available"`
	Failed         int                            `json:"failed"`
	ReloadRequired bool                           `json:"reloadRequired"`
}

type NodeHealthProbeAllDTO struct {
	Items          []*ProxyNodeHealthDTO `json:"items"`
	Total          int                   `json:"total"`
	Queued         int                   `json:"queued"`
	Available      int                   `json:"available"`
	Failed         int                   `json:"failed"`
	ReloadRequired bool                  `json:"reloadRequired"`
}

type NodeBlacklistRequest struct {
	Duration string `json:"duration,omitempty" doc:"拉黑时长，例如 30m、1h；为空使用配置默认值"`
}

type ProxyTestRequest struct {
	ProbeURL string `json:"probeUrl,omitempty" doc:"用于测试代理可访问性的 HTTP/HTTPS URL"`
}

type ProxyTestResultDTO struct {
	TargetType string              `json:"targetType"`
	TargetID   string              `json:"targetId"`
	TargetName string              `json:"targetName"`
	ProbeURL   string              `json:"probeUrl"`
	Available  bool                `json:"available"`
	LatencyMs  int64               `json:"latencyMs"`
	Error      string              `json:"error,omitempty"`
	CheckedAt  time.Time           `json:"checkedAt"`
	Health     *ProxyNodeHealthDTO `json:"health,omitempty"`
	NodeID     string              `json:"nodeId,omitempty"`
	NodeName   string              `json:"nodeName,omitempty"`
	NodeTag    string              `json:"nodeTag,omitempty"`
	NodeError  string              `json:"nodeError,omitempty"`
}

type SubscriptionUpsertRequest struct {
	Name    string `json:"name,omitempty" validate:"omitempty,max=100"`
	URL     string `json:"url" validate:"required,max=2000"`
	GroupID string `json:"groupId,omitempty"`
	Remark  string `json:"remark,omitempty" validate:"omitempty,max=500"`
}

type SubscriptionSyncRequest struct {
	Raw string `json:"raw,omitempty" doc:"可选，直接用文本内容同步，便于测试或离线导入"`
}

type GroupUpsertRequest struct {
	Name     string   `json:"name" validate:"required,max=100"`
	Strategy string   `json:"strategy,omitempty"`
	NodeIDs  []string `json:"nodeIds,omitempty"`
	Remark   string   `json:"remark,omitempty" validate:"omitempty,max=500"`
}

type MappingUpsertRequest struct {
	Enabled          bool     `json:"enabled"`
	ListenAddress    string   `json:"listenAddress" validate:"required,max=64"`
	ListenPort       uint16   `json:"listenPort" validate:"required,min=1,max=65535"`
	OutboundProtocol string   `json:"outboundProtocol" validate:"required"`
	Username         string   `json:"username,omitempty" validate:"omitempty,max=255"`
	Password         string   `json:"password,omitempty" validate:"omitempty,max=500"`
	Strategy         string   `json:"strategy" validate:"required"`
	NodeIDs          []string `json:"nodeIds,omitempty"`
	ActiveNodeID     *string  `json:"activeNodeId,omitempty"`
	GroupIDs         []string `json:"groupIds,omitempty"`
	ActiveGroupID    *string  `json:"activeGroupId,omitempty"`
	Remark           string   `json:"remark,omitempty" validate:"omitempty,max=500"`
}

const (
	MappingSwitchTargetNode  = "node"
	MappingSwitchTargetGroup = "group"
)

type MappingSwitchRequest struct {
	TargetType string `json:"targetType" validate:"required"`
	TargetID   string `json:"targetId" validate:"required"`
}

func ToNodeDTO(node *tables.ProxyNodeTable) *ProxyNodeDTO {
	if node == nil {
		return nil
	}
	chainMembers := chainMembersForNode(node)
	return &ProxyNodeDTO{
		ID:             node.ID,
		Name:           node.Name,
		Protocol:       node.Protocol,
		Server:         node.Server,
		Port:           node.Port,
		Username:       node.Username,
		Password:       node.Password,
		RawURI:         node.RawURI,
		Tags:           decodeStringSlice(node.TagsJSON),
		Remark:         node.Remark,
		ChainNodeIDs:   chainNodeIDsFromMembers(chainMembers),
		ChainMembers:   chainMembers,
		SubscriptionID: node.SubscriptionID,
		GroupID:        node.GroupID,
		GroupIDs:       stringSliceOrEmpty(node.GroupID),
		SourceKey:      node.SourceKey,
		CreatedAt:      node.CreatedAt,
		UpdatedAt:      node.UpdatedAt,
	}
}

func ToNodeDTOWithGroups(node *tables.ProxyNodeTable, groups []*tables.ProxyGroupTable) *ProxyNodeDTO {
	dto := ToNodeDTO(node)
	if dto == nil {
		return nil
	}
	dto.GroupIDs = groupIDsForNodeFromGroups(node.ID, node.GroupID, groups)
	return dto
}

func ToNodeDTOWithHealth(node *tables.ProxyNodeTable, health *tables.ProxyNodeHealthTable) *ProxyNodeDTO {
	dto := ToNodeDTO(node)
	if dto == nil {
		return nil
	}
	dto.Health = ToNodeHealthDTO(health)
	return dto
}

func ToNodeDTOWithHealthAndGroups(node *tables.ProxyNodeTable, health *tables.ProxyNodeHealthTable, groups []*tables.ProxyGroupTable) *ProxyNodeDTO {
	dto := ToNodeDTOWithGroups(node, groups)
	if dto == nil {
		return nil
	}
	dto.Health = ToNodeHealthDTO(health)
	return dto
}

func ToNodeOptionDTO(node *tables.ProxyNodeTable, groups []*tables.ProxyGroupTable) *ProxyNodeOptionDTO {
	if node == nil {
		return nil
	}
	return &ProxyNodeOptionDTO{
		ID:       node.ID,
		Name:     node.Name,
		Protocol: node.Protocol,
		Server:   node.Server,
		Port:     node.Port,
		GroupIDs: groupIDsForNodeFromGroups(node.ID, node.GroupID, groups),
	}
}

func ToNodeHealthDTO(health *tables.ProxyNodeHealthTable) *ProxyNodeHealthDTO {
	if health == nil {
		return nil
	}
	return &ProxyNodeHealthDTO{
		NodeID:                  health.NodeID,
		Available:               health.Available,
		FailureCount:            health.FailureCount,
		SuccessCount:            health.SuccessCount,
		ConsecutiveFailureCount: health.ConsecutiveFailureCount,
		Blacklisted:             health.Blacklisted,
		BlacklistedUntil:        health.BlacklistedUntil,
		LastLatencyMs:           health.LastLatencyMs,
		LastError:               health.LastError,
		LastCheckedAt:           health.LastCheckedAt,
		LastSuccessAt:           health.LastSuccessAt,
		LastFailureAt:           health.LastFailureAt,
		UpdatedAt:               health.UpdatedAt,
	}
}

func ToNodeHealthDTOs(rows []*tables.ProxyNodeHealthTable) []*ProxyNodeHealthDTO {
	items := make([]*ProxyNodeHealthDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, ToNodeHealthDTO(row))
	}
	return items
}

func ToNodeHealthProbeAllDTO(result *NodeHealthProbeAllResult) *NodeHealthProbeAllDTO {
	if result == nil {
		return nil
	}
	return &NodeHealthProbeAllDTO{
		Items:          ToNodeHealthDTOs(result.Items),
		Total:          result.Total,
		Queued:         result.Queued,
		Available:      result.Available,
		Failed:         result.Failed,
		ReloadRequired: result.ReloadRequired,
	}
}

func ToSubscriptionDTO(subscription *tables.ProxySubscriptionTable) *ProxySubscriptionDTO {
	if subscription == nil {
		return nil
	}
	return &ProxySubscriptionDTO{
		ID:             subscription.ID,
		Name:           subscription.Name,
		URL:            subscription.URL,
		GroupID:        subscription.GroupID,
		Remark:         subscription.Remark,
		LastSyncedAt:   subscription.LastSyncedAt,
		LastSyncStatus: subscription.LastSyncStatus,
		LastSyncError:  subscription.LastSyncError,
		CreatedAt:      subscription.CreatedAt,
		UpdatedAt:      subscription.UpdatedAt,
	}
}

func ToGroupDTO(group *tables.ProxyGroupTable) *ProxyGroupDTO {
	if group == nil {
		return nil
	}
	return &ProxyGroupDTO{
		ID:             group.ID,
		Name:           group.Name,
		Type:           group.Type,
		Strategy:       group.Strategy,
		SubscriptionID: group.SubscriptionID,
		SourceKey:      group.SourceKey,
		NodeIDs:        decodeStringSlice(group.NodeIDsJSON),
		GroupIDs:       decodeStringSlice(group.GroupIDsJSON),
		BuiltinTags:    decodeStringSlice(group.BuiltinTagsJSON),
		IncludesAll:    group.IncludesAll,
		Filter:         group.Filter,
		Remark:         group.Remark,
		NodeCount:      len(decodeStringSlice(group.NodeIDsJSON)),
		CreatedAt:      group.CreatedAt,
		UpdatedAt:      group.UpdatedAt,
	}
}

func ToMappingDTO(mapping *tables.PortMappingTable) *PortMappingDTO {
	if mapping == nil {
		return nil
	}
	activeNodeID := stringPtrOrNil(mapping.ActiveNodeID)
	activeGroupID := stringPtrOrNil(mapping.ActiveGroupID)
	return &PortMappingDTO{
		ID:               mapping.ID,
		Enabled:          mapping.Enabled,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		Order:            mapping.Order,
		OutboundProtocol: mapping.OutboundProtocol,
		Username:         mapping.Username,
		Password:         mapping.Password,
		Strategy:         mapping.Strategy,
		NodeIDs:          decodeStringSlice(mapping.NodeIDsJSON),
		ActiveNodeID:     activeNodeID,
		GroupIDs:         decodeStringSlice(mapping.GroupIDsJSON),
		ActiveGroupID:    activeGroupID,
		Remark:           mapping.Remark,
		CreatedAt:        mapping.CreatedAt,
		UpdatedAt:        mapping.UpdatedAt,
	}
}

func ToNodeDTOs(nodes []*tables.ProxyNodeTable) []*ProxyNodeDTO {
	items := make([]*ProxyNodeDTO, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, ToNodeDTO(node))
	}
	return items
}

func ToNodeDTOsWithGroups(nodes []*tables.ProxyNodeTable, groups []*tables.ProxyGroupTable) []*ProxyNodeDTO {
	items := make([]*ProxyNodeDTO, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, ToNodeDTOWithGroups(node, groups))
	}
	return items
}

func ToNodeDTOsWithHealth(nodes []*tables.ProxyNodeTable, healthByNodeID map[string]*tables.ProxyNodeHealthTable) []*ProxyNodeDTO {
	items := make([]*ProxyNodeDTO, 0, len(nodes))
	for _, node := range nodes {
		var health *tables.ProxyNodeHealthTable
		if healthByNodeID != nil && node != nil {
			health = healthByNodeID[node.ID]
		}
		items = append(items, ToNodeDTOWithHealth(node, health))
	}
	return items
}

func ToNodeDTOsWithHealthAndGroups(nodes []*tables.ProxyNodeTable, healthByNodeID map[string]*tables.ProxyNodeHealthTable, groups []*tables.ProxyGroupTable) []*ProxyNodeDTO {
	items := make([]*ProxyNodeDTO, 0, len(nodes))
	for _, node := range nodes {
		var health *tables.ProxyNodeHealthTable
		if healthByNodeID != nil && node != nil {
			health = healthByNodeID[node.ID]
		}
		items = append(items, ToNodeDTOWithHealthAndGroups(node, health, groups))
	}
	return items
}

func ToNodeOptionDTOs(nodes []*tables.ProxyNodeTable, groups []*tables.ProxyGroupTable) []*ProxyNodeOptionDTO {
	items := make([]*ProxyNodeOptionDTO, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, ToNodeOptionDTO(node, groups))
	}
	return items
}

func ToSubscriptionDTOs(subscriptions []*tables.ProxySubscriptionTable) []*ProxySubscriptionDTO {
	items := make([]*ProxySubscriptionDTO, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		items = append(items, ToSubscriptionDTO(subscription))
	}
	return items
}

func ToGroupDTOs(groups []*tables.ProxyGroupTable) []*ProxyGroupDTO {
	items := make([]*ProxyGroupDTO, 0, len(groups))
	for _, group := range groups {
		items = append(items, ToGroupDTO(group))
	}
	return items
}

func ToMappingDTOs(mappings []*tables.PortMappingTable) []*PortMappingDTO {
	items := make([]*PortMappingDTO, 0, len(mappings))
	for _, mapping := range mappings {
		items = append(items, ToMappingDTO(mapping))
	}
	return items
}

func encodeStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeStringSlice(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	if values == nil {
		return []string{}
	}
	return values
}

func encodeChainMembers(members []ChainMemberDTO) string {
	members = normalizeChainMembers(members)
	if len(members) == 0 {
		return "[]"
	}
	data, err := json.Marshal(members)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeChainMembers(raw string) []ChainMemberDTO {
	if raw == "" {
		return []ChainMemberDTO{}
	}
	var members []ChainMemberDTO
	if err := json.Unmarshal([]byte(raw), &members); err != nil {
		return []ChainMemberDTO{}
	}
	return normalizeChainMembers(members)
}

func chainMembersForNode(node *tables.ProxyNodeTable) []ChainMemberDTO {
	if node == nil {
		return []ChainMemberDTO{}
	}
	if members := decodeChainMembers(node.ChainMembersJSON); len(members) > 0 {
		return members
	}
	return chainMembersFromNodeIDs(decodeStringSlice(node.ChainNodeIDsJSON))
}

func chainMembersFromNodeIDs(nodeIDs []string) []ChainMemberDTO {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	members := make([]ChainMemberDTO, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		members = append(members, ChainMemberDTO{Type: ChainMemberTypeNode, ID: nodeID})
	}
	return members
}

func chainNodeIDsFromMembers(members []ChainMemberDTO) []string {
	members = normalizeChainMembers(members)
	nodeIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member.Type == ChainMemberTypeNode {
			nodeIDs = append(nodeIDs, member.ID)
		}
	}
	return nodeIDs
}

func chainGroupIDsFromMembers(members []ChainMemberDTO) []string {
	members = normalizeChainMembers(members)
	groupIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member.Type == ChainMemberTypeGroup {
			groupIDs = append(groupIDs, member.ID)
		}
	}
	return groupIDs
}

func normalizeChainMembers(members []ChainMemberDTO) []ChainMemberDTO {
	seen := map[string]struct{}{}
	result := make([]ChainMemberDTO, 0, len(members))
	for _, member := range members {
		member.Type = normalizeChainMemberType(member.Type)
		member.ID = strings.TrimSpace(member.ID)
		if member.Type == "" || member.ID == "" {
			continue
		}
		key := member.Type + ":" + member.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, member)
	}
	return result
}

func normalizeChainMemberType(value string) string {
	switch strings.TrimSpace(value) {
	case ChainMemberTypeNode:
		return ChainMemberTypeNode
	case ChainMemberTypeGroup:
		return ChainMemberTypeGroup
	default:
		return ""
	}
}

func stringSliceOrEmpty(value string) []string {
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func groupIDsForNodeFromGroups(nodeID string, legacyGroupID string, groups []*tables.ProxyGroupTable) []string {
	values := stringSliceOrEmpty(legacyGroupID)
	for _, group := range groups {
		if group == nil || !containsString(decodeStringSlice(group.NodeIDsJSON), nodeID) {
			continue
		}
		values = append(values, group.ID)
	}
	return uniqueNonEmpty(values)
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
