package tables

import (
	"time"

	"proxy-hub/utils/model_base"
)

// ProxyNodeHealthTable stores the latest health probe result for a proxy node.
type ProxyNodeHealthTable struct {
	model_base.StringPKBaseModel

	NodeID           string     `gorm:"type:text;not null;uniqueIndex" json:"nodeId"`
	Available        bool       `gorm:"not null;default:false" json:"available"`
	FailureCount     int        `gorm:"not null;default:0" json:"failureCount"`
	SuccessCount     int64      `gorm:"not null;default:0" json:"successCount"`
	Blacklisted      bool       `gorm:"not null;default:false;index" json:"blacklisted"`
	BlacklistedUntil *time.Time `gorm:"index" json:"blacklistedUntil"`
	LastLatencyMs    int64      `gorm:"not null;default:0" json:"lastLatencyMs"`
	LastError        string     `gorm:"type:text" json:"lastError"`
	LastCheckedAt    *time.Time `json:"lastCheckedAt"`
	LastSuccessAt    *time.Time `json:"lastSuccessAt"`
	LastFailureAt    *time.Time `json:"lastFailureAt"`
}

func (*ProxyNodeHealthTable) TableName() string {
	return "proxy_node_health"
}
