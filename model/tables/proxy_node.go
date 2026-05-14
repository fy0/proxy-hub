package tables

import "proxy-hub/utils/model_base"

// ProxyNodeTable stores imported or manually configured upstream proxy nodes.
type ProxyNodeTable struct {
	model_base.StringPKBaseModel

	Name     string  `gorm:"type:text;not null" json:"name"`
	Protocol string  `gorm:"type:text;not null;index" json:"protocol"`
	Server   string  `gorm:"type:text;not null" json:"server"`
	Port     *uint16 `json:"port"`

	Username string `gorm:"type:text" json:"username"`
	Password string `gorm:"type:text" json:"-"`
	RawURI   string `gorm:"type:text" json:"rawUri"`
	TagsJSON string `gorm:"type:text" json:"-"`
	Remark   string `gorm:"type:text" json:"remark"`
}

func (*ProxyNodeTable) TableName() string {
	return "proxy_nodes"
}
