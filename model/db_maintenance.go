package model

import (
	"context"
	"fmt"
	"os"
	"strings"

	"proxy-hub/model/tables"
)

type DBCompactTableResult struct {
	TableName string `json:"tableName"`
	Deleted   int64  `json:"deleted"`
}

type DBCompactResult struct {
	BeforeBytes int64                  `json:"beforeBytes"`
	AfterBytes  int64                  `json:"afterBytes"`
	Tables      []DBCompactTableResult `json:"tables"`
}

func CompactDB(ctx context.Context, dsn string) (*DBCompactResult, error) {
	if db == nil {
		return nil, ErrDBNotReady
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result := &DBCompactResult{
		BeforeBytes: sqliteDBFileSize(dsn),
		Tables: []DBCompactTableResult{
			{TableName: (&tables.UserTable{}).TableName()},
			{TableName: (&tables.UserAccessTokenTable{}).TableName()},
			{TableName: (&tables.PortMappingTable{}).TableName()},
			{TableName: (&tables.ProxyNodeHealthTable{}).TableName()},
			{TableName: (&tables.ProxyNodeHealthHistoryTable{}).TableName()},
			{TableName: (&tables.ProxyNodeTable{}).TableName()},
			{TableName: (&tables.ProxyGroupTable{}).TableName()},
			{TableName: (&tables.ProxySubscriptionTable{}).TableName()},
		},
	}

	err := Transaction(ctx, func(tx DBTx) error {
		for index := range result.Tables {
			deleted, err := purgeSoftDeletedRows(ctx, tx, result.Tables[index].TableName)
			if err != nil {
				return err
			}
			result.Tables[index].Deleted = deleted
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if db.Dialector != nil && db.Dialector.Name() == "sqlite" {
		if err := db.WithContext(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
			return nil, err
		}
		if err := db.WithContext(ctx).Exec("VACUUM").Error; err != nil {
			return nil, err
		}
		if err := db.WithContext(ctx).Exec("PRAGMA optimize").Error; err != nil {
			return nil, err
		}
		if err := db.WithContext(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
			return nil, err
		}
	}

	result.AfterBytes = sqliteDBFileSize(dsn)
	return result, nil
}

func purgeSoftDeletedRows(ctx context.Context, tx DBTx, tableName string) (int64, error) {
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		return 0, nil
	}
	if !isCompactTableName(tableName) {
		return 0, fmt.Errorf("compact db: unsupported table %q", tableName)
	}
	result := tx.WithContext(ctx).Exec("DELETE FROM " + tableName + " WHERE deleted_at IS NOT NULL")
	return result.RowsAffected, result.Error
}

func isCompactTableName(tableName string) bool {
	switch tableName {
	case "users",
		"user_access_tokens",
		"port_mappings",
		"proxy_node_health",
		"proxy_node_health_history",
		"proxy_nodes",
		"proxy_groups",
		"proxy_subscriptions":
		return true
	default:
		return false
	}
}

func sqliteDBFileSize(dsn string) int64 {
	path := sqliteDBPath(dsn)
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

func sqliteDBPath(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" || strings.HasPrefix(dsn, ":memory:") {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(dsn), "file:") {
		value := strings.TrimPrefix(dsn, "file:")
		if index := strings.Index(value, "?"); index >= 0 {
			value = value[:index]
		}
		return value
	}
	if strings.HasSuffix(strings.ToLower(dsn), ".db") {
		return dsn
	}
	return ""
}
