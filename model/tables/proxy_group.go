package tables

import "proxy-hub/utils/model_base"

// ProxyGroupTable stores manual node groups and imported Clash/Mihomo proxy groups.
type ProxyGroupTable struct {
	model_base.StringPKBaseModel

	Name            string `gorm:"type:text;not null;index" json:"name"`
	Type            string `gorm:"type:text;not null;index" json:"type"`
	Strategy        string `gorm:"type:text;not null" json:"strategy"`
	SubscriptionID  string `gorm:"type:text;index" json:"subscriptionId"`
	SourceKey       string `gorm:"type:text;index" json:"sourceKey"`
	NodeIDsJSON     string `gorm:"type:text" json:"-"`
	GroupIDsJSON    string `gorm:"type:text" json:"-"`
	BuiltinTagsJSON string `gorm:"type:text" json:"-"`
	IncludesAll     bool   `gorm:"not null;default:false" json:"includesAll"`
	Filter          string `gorm:"type:text" json:"filter"`
	Remark          string `gorm:"type:text" json:"remark"`
}

func (*ProxyGroupTable) TableName() string {
	return "proxy_groups"
}
