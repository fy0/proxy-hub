package tables

import (
	"time"

	"proxy-hub/utils/model_base"
)

// ProxyNodeHealthHistoryTable stores bounded probe history for one proxy node.
type ProxyNodeHealthHistoryTable struct {
	model_base.StringPKBaseModel

	NodeID    string    `gorm:"type:text;not null;index:idx_node_health_history_node_checked" json:"nodeId"`
	Source    string    `gorm:"type:text;not null;index" json:"source"`
	Available bool      `gorm:"not null;default:false" json:"available"`
	LatencyMs int64     `gorm:"not null;default:0" json:"latencyMs"`
	Error     string    `gorm:"type:text" json:"error"`
	ProbeURL  string    `gorm:"type:text" json:"probeUrl"`
	TargetID  string    `gorm:"type:text;index" json:"targetId"`
	CheckedAt time.Time `gorm:"not null;index:idx_node_health_history_node_checked,sort:desc" json:"checkedAt"`
}

func (*ProxyNodeHealthHistoryTable) TableName() string {
	return "proxy_node_health_history"
}
