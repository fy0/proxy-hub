package tables

import (
	"time"

	"proxy-hub/utils/model_base"
)

// ProxySubscriptionTable stores remote subscription metadata used to sync nodes and groups.
type ProxySubscriptionTable struct {
	model_base.StringPKBaseModel

	Name           string     `gorm:"type:text;not null" json:"name"`
	URL            string     `gorm:"type:text;not null" json:"url"`
	GroupID        string     `gorm:"type:text;index" json:"groupId"`
	Remark         string     `gorm:"type:text" json:"remark"`
	LastSyncedAt   *time.Time `json:"lastSyncedAt"`
	LastSyncStatus string     `gorm:"type:text" json:"lastSyncStatus"`
	LastSyncError  string     `gorm:"type:text" json:"lastSyncError"`
}

func (*ProxySubscriptionTable) TableName() string {
	return "proxy_subscriptions"
}
