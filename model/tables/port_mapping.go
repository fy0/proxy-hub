package tables

import "proxy-hub/utils/model_base"

// PortMappingTable stores local inbound ports served by the embedded sing-box runtime.
type PortMappingTable struct {
	model_base.StringPKBaseModel

	Enabled          bool   `gorm:"not null;default:true;index" json:"enabled"`
	ListenAddress    string `gorm:"type:text;not null" json:"listenAddress"`
	ListenPort       uint16 `gorm:"not null;uniqueIndex:idx_port_mappings_listen" json:"listenPort"`
	OutboundProtocol string `gorm:"type:text;not null" json:"outboundProtocol"`
	Username         string `gorm:"type:text" json:"username"`
	Password         string `gorm:"type:text" json:"-"`
	Strategy         string `gorm:"type:text;not null" json:"strategy"`
	NodeIDsJSON      string `gorm:"type:text" json:"-"`
	ActiveNodeID     string `gorm:"type:text" json:"activeNodeId"`
	Remark           string `gorm:"type:text" json:"remark"`
}

func (*PortMappingTable) TableName() string {
	return "port_mappings"
}
