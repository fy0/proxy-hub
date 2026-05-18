package model

import (
	"proxy-hub/model/tables"
)

func GetAllModels() []any {
	return []any{
		&tables.UserTable{},
		&tables.UserAccessTokenTable{},
		&tables.ProxySubscriptionTable{},
		&tables.ProxyNodeTable{},
		&tables.ProxyNodeHealthTable{},
		&tables.ProxyNodeHealthHistoryTable{},
		&tables.ProxyGroupTable{},
		&tables.PortMappingTable{},
	}
}

func DBMigrate(autoMigrate bool) {
	if !autoMigrate {
		return
	}

	db.AutoMigrate(GetAllModels()...)
}
