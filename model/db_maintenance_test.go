package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxy-hub/model/tables"

	"gorm.io/gorm/logger"
)

func TestCompactDBPurgesSoftDeletedProxyRows(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "compact.db")
	if err := InitWithDSN(dsn, int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN() error = %v", err)
	}
	t.Cleanup(DBClose)

	node := &tables.ProxyNodeTable{
		Name:     "edge",
		Protocol: "http",
		Server:   "127.0.0.1",
	}
	if err := GetTx(nil).Create(node).Error; err != nil {
		t.Fatalf("create node error = %v", err)
	}
	user := &tables.UserTable{
		Username: "alice",
		Password: "secret",
		Salt:     "salt",
	}
	if err := GetTx(nil).Create(user).Error; err != nil {
		t.Fatalf("create user error = %v", err)
	}
	health := &tables.ProxyNodeHealthTable{
		NodeID:        node.ID,
		Available:     true,
		LastLatencyMs: 42,
	}
	if err := GetTx(nil).Create(health).Error; err != nil {
		t.Fatalf("create health error = %v", err)
	}
	history := &tables.ProxyNodeHealthHistoryTable{
		NodeID:    node.ID,
		Source:    "node-test",
		Available: true,
		ProbeURL:  "https://example.com/generate_204",
		TargetID:  node.ID,
		CheckedAt: time.Now(),
	}
	if err := GetTx(nil).Create(history).Error; err != nil {
		t.Fatalf("create history error = %v", err)
	}

	if err := GetTx(nil).Delete(health).Error; err != nil {
		t.Fatalf("soft delete health error = %v", err)
	}
	if err := GetTx(nil).Delete(history).Error; err != nil {
		t.Fatalf("soft delete history error = %v", err)
	}
	if err := GetTx(nil).Delete(node).Error; err != nil {
		t.Fatalf("soft delete node error = %v", err)
	}
	if err := GetTx(nil).Delete(user).Error; err != nil {
		t.Fatalf("soft delete user error = %v", err)
	}

	result, err := CompactDB(context.Background(), dsn)
	if err != nil {
		t.Fatalf("CompactDB() error = %v", err)
	}
	if deletedForTable(result, "proxy_nodes") != 1 ||
		deletedForTable(result, "proxy_node_health") != 1 ||
		deletedForTable(result, "proxy_node_health_history") != 1 {
		t.Fatalf("CompactDB() table result = %+v, want deleted proxy rows", result.Tables)
	}
	if deletedForTable(result, "users") != 1 {
		t.Fatalf("CompactDB() users deleted = %d, want 1", deletedForTable(result, "users"))
	}
	assertModelUnscopedCount(t, &tables.ProxyNodeTable{}, 0)
	assertModelUnscopedCount(t, &tables.ProxyNodeHealthTable{}, 0)
	assertModelUnscopedCount(t, &tables.ProxyNodeHealthHistoryTable{}, 0)
	assertModelUnscopedCount(t, &tables.UserTable{}, 0)
}

func deletedForTable(result *DBCompactResult, tableName string) int64 {
	if result == nil {
		return 0
	}
	for _, table := range result.Tables {
		if table.TableName == tableName {
			return table.Deleted
		}
	}
	return 0
}

func assertModelUnscopedCount(t *testing.T, table any, want int64) {
	t.Helper()

	var count int64
	if err := GetTx(nil).Unscoped().Model(table).Count(&count).Error; err != nil {
		t.Fatalf("count %T error = %v", table, err)
	}
	if count != want {
		t.Fatalf("count %T = %d, want %d", table, count, want)
	}
}
