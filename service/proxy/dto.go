package proxy

import (
	"encoding/json"
	"time"

	"proxy-hub/model/tables"
)

const (
	ProtocolVLESS   = "vless"
	ProtocolVMess   = "vmess"
	ProtocolTrojan  = "trojan"
	ProtocolSOCKS5  = "socks5"
	ProtocolHTTP    = "http"
	ProtocolUnknown = "unknown"

	OutboundProtocolMixed = "mixed"
	OutboundProtocolSOCKS = "socks5"
	OutboundProtocolHTTP  = "http"

	StrategyFailover    = "failover"
	StrategyLoadBalance = "load-balance"
	StrategyManual      = "manual"

	GroupTypeManual       = "manual"
	GroupTypeSubscription = "subscription"

	GroupStrategySelector = "selector"
	GroupStrategyURLTest  = "url-test"

	SubscriptionSyncStatusSuccess = "success"
	SubscriptionSyncStatusFailed  = "failed"
)

type ProxyNodeDTO struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Protocol       string    `json:"protocol"`
	Server         string    `json:"server"`
	Port           *uint16   `json:"port"`
	Username       string    `json:"username"`
	Password       string    `json:"password"`
	RawURI         string    `json:"rawUri"`
	Tags           []string  `json:"tags"`
	Remark         string    `json:"remark"`
	SubscriptionID string    `json:"subscriptionId"`
	GroupID        string    `json:"groupId"`
	SourceKey      string    `json:"sourceKey"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
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
}

type NodeUpsertRequest struct {
	Name           string   `json:"name,omitempty" validate:"omitempty,max=100"`
	Protocol       string   `json:"protocol,omitempty" validate:"omitempty"`
	Server         string   `json:"server,omitempty" validate:"omitempty,max=255"`
	Port           *uint16  `json:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	Username       string   `json:"username,omitempty" validate:"omitempty,max=255"`
	Password       string   `json:"password,omitempty" validate:"omitempty,max=500"`
	RawURI         string   `json:"rawUri,omitempty" validate:"omitempty"`
	Tags           []string `json:"tags,omitempty" validate:"omitempty"`
	Remark         string   `json:"remark,omitempty" validate:"omitempty,max=500"`
	SubscriptionID string   `json:"-"`
	GroupID        string   `json:"groupId,omitempty"`
	SourceKey      string   `json:"-"`
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

type NodeImportResult struct {
	Items    []*ProxyNodeDTO     `json:"items"`
	Groups   []*ProxyGroupDTO    `json:"groups"`
	Failures []NodeImportFailure `json:"failures"`
	Total    int                 `json:"total"`
	Imported int                 `json:"imported"`
	Failed   int                 `json:"failed"`
	Updated  int                 `json:"updated"`
	Deleted  int                 `json:"deleted"`
	Skipped  int                 `json:"skipped"`
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
	GroupIDs []string `json:"groupIds,omitempty"`
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

func ToNodeDTO(node *tables.ProxyNodeTable) *ProxyNodeDTO {
	if node == nil {
		return nil
	}
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
		SubscriptionID: node.SubscriptionID,
		GroupID:        node.GroupID,
		SourceKey:      node.SourceKey,
		CreatedAt:      node.CreatedAt,
		UpdatedAt:      node.UpdatedAt,
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

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
