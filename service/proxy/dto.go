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
)

type ProxyNodeDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Protocol  string    `json:"protocol"`
	Server    string    `json:"server"`
	Port      *uint16   `json:"port"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	RawURI    string    `json:"rawUri"`
	Tags      []string  `json:"tags"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PortMappingDTO struct {
	ID               string    `json:"id"`
	Enabled          bool      `json:"enabled"`
	ListenAddress    string    `json:"listenAddress"`
	ListenPort       uint16    `json:"listenPort"`
	OutboundProtocol string    `json:"outboundProtocol"`
	Username         string    `json:"username"`
	Password         string    `json:"password"`
	Strategy         string    `json:"strategy"`
	NodeIDs          []string  `json:"nodeIds"`
	ActiveNodeID     *string   `json:"activeNodeId"`
	Remark           string    `json:"remark"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type StateSnapshotDTO struct {
	Nodes       []*ProxyNodeDTO   `json:"nodes"`
	Mappings    []*PortMappingDTO `json:"mappings"`
	Runtime     RuntimeStatus     `json:"runtime"`
	LastSavedAt time.Time         `json:"lastSavedAt"`
}

type NodeUpsertRequest struct {
	Name     string   `json:"name" validate:"required,min=1,max=100"`
	Protocol string   `json:"protocol" validate:"required"`
	Server   string   `json:"server" validate:"omitempty,max=255"`
	Port     *uint16  `json:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	Username string   `json:"username,omitempty" validate:"omitempty,max=255"`
	Password string   `json:"password,omitempty" validate:"omitempty,max=500"`
	RawURI   string   `json:"rawUri,omitempty" validate:"omitempty"`
	Tags     []string `json:"tags,omitempty" validate:"omitempty"`
	Remark   string   `json:"remark,omitempty" validate:"omitempty,max=500"`
}

type NodeImportRequest struct {
	Raw  string   `json:"raw,omitempty" doc:"多行分享链接文本"`
	URIs []string `json:"uris,omitempty" doc:"分享链接列表"`
}

type NodeImportFailure struct {
	URI     string `json:"uri"`
	Message string `json:"message"`
}

type NodeImportResult struct {
	Items    []*ProxyNodeDTO     `json:"items"`
	Failures []NodeImportFailure `json:"failures"`
	Total    int                 `json:"total"`
	Imported int                 `json:"imported"`
	Failed   int                 `json:"failed"`
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
	Remark           string   `json:"remark,omitempty" validate:"omitempty,max=500"`
}

func ToNodeDTO(node *tables.ProxyNodeTable) *ProxyNodeDTO {
	if node == nil {
		return nil
	}
	return &ProxyNodeDTO{
		ID:        node.ID,
		Name:      node.Name,
		Protocol:  node.Protocol,
		Server:    node.Server,
		Port:      node.Port,
		Username:  node.Username,
		Password:  node.Password,
		RawURI:    node.RawURI,
		Tags:      decodeStringSlice(node.TagsJSON),
		Remark:    node.Remark,
		CreatedAt: node.CreatedAt,
		UpdatedAt: node.UpdatedAt,
	}
}

func ToMappingDTO(mapping *tables.PortMappingTable) *PortMappingDTO {
	if mapping == nil {
		return nil
	}
	activeNodeID := stringPtrOrNil(mapping.ActiveNodeID)
	return &PortMappingDTO{
		ID:               mapping.ID,
		Enabled:          mapping.Enabled,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Username:         mapping.Username,
		Password:         mapping.Password,
		Strategy:         mapping.Strategy,
		NodeIDs:          decodeStringSlice(mapping.NodeIDsJSON),
		ActiveNodeID:     activeNodeID,
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
