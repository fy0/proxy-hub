package proxy

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestSettingsImportZipRoundTripReplacesExistingConfig(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "edge",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1080),
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "manual", NodeIDs: []string{node.ID}})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	if _, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10090,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{node.ID},
		ActiveNodeID:     &node.ID,
		GroupIDs:         []string{group.ID},
		GroupStrategyOverrides: map[string]string{
			group.ID: GroupStrategyOverrideLoadBalance,
		},
		ActiveGroupID: &group.ID,
	}); err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	rawZip, err := SettingsExportZip(ctx, nil)
	if err != nil {
		t.Fatalf("SettingsExportZip() error = %v", err)
	}
	backup, err := SettingsBackupFromZip(rawZip)
	if err != nil {
		t.Fatalf("SettingsBackupFromZip() error = %v", err)
	}
	if len(backup.Data.Nodes) != 1 || len(backup.Data.Groups) != 1 || len(backup.Data.Mappings) != 1 {
		t.Fatalf("backup data = %+v, want one node/group/mapping", backup.Data)
	}
	if backup.Data.Mappings[0].GroupStrategyOverrides[group.ID] != GroupStrategyOverrideLoadBalance {
		t.Fatalf("backup mapping overrides = %+v, want load-balance", backup.Data.Mappings[0].GroupStrategyOverrides)
	}

	extra, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "extra",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(extra) error = %v", err)
	}

	result, err := SettingsImportZip(ctx, rawZip)
	if err != nil {
		t.Fatalf("SettingsImportZip() error = %v", err)
	}
	if result.Nodes != 1 || result.Groups != 1 || result.Mappings != 1 {
		t.Fatalf("SettingsImportZip() result = %+v, want one node/group/mapping", result)
	}
	importedMapping, err := MappingGet(ctx, nil, backup.Data.Mappings[0].ID)
	if err != nil {
		t.Fatalf("MappingGet(imported) error = %v", err)
	}
	if decodeGroupStrategyOverrides(importedMapping.GroupStrategyOverridesJSON)[group.ID] != GroupStrategyOverrideLoadBalance {
		t.Fatalf("imported mapping overrides = %q, want load-balance", importedMapping.GroupStrategyOverridesJSON)
	}
	if _, err := NodeGet(ctx, nil, extra.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("NodeGet(extra) error = %v, want %v", err, ErrNodeNotFound)
	}
}

func TestSettingsImportZipRejectsInvalidArchiveWithoutMutation(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	original, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "original",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1080),
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}

	cases := map[string][]byte{
		"not zip": []byte("not a zip"),
		"no json": mustZipWithEntries(t, map[string]string{
			"notes.txt": "backup",
		}),
		"multiple json": mustZipWithEntries(t, map[string]string{
			"a.json": "{}",
			"b.json": "{}",
		}),
		"invalid json": mustZipWithEntries(t, map[string]string{
			settingsBackupZipEntryName: "{",
		}),
		"invalid backup": mustZipWithEntries(t, map[string]string{
			settingsBackupZipEntryName: `{"kind":"wrong","schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z","data":{"nodes":[],"groups":[],"subscriptions":[],"mappings":[]}}`,
		}),
	}

	for name, rawZip := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := SettingsImportZip(ctx, rawZip)
			if !errors.Is(err, ErrInvalidSettingsBackup) {
				t.Fatalf("SettingsImportZip() error = %v, want %v", err, ErrInvalidSettingsBackup)
			}
			nodes, err := NodeList(ctx, nil)
			if err != nil {
				t.Fatalf("NodeList() error = %v", err)
			}
			if len(nodes) != 1 || nodes[0].ID != original.ID {
				t.Fatalf("nodes after rejected import = %+v, want original data unchanged", nodes)
			}
		})
	}
}

func mustZipWithEntries(t *testing.T, entries map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buffer.Bytes()
}
