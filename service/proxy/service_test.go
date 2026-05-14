package proxy

import (
	"context"
	"encoding/base64"
	"testing"

	"proxy-hub/model"

	"gorm.io/gorm/logger"
)

func initProxyInMemoryDB(t *testing.T) {
	t.Helper()

	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
}

func TestNodeCreateFromRawURI(t *testing.T) {
	initProxyInMemoryDB(t)

	raw := "vless://uuid@example.com:443?type=ws&security=tls&sni=edge.example.com&path=%2Fproxy#edge"
	node, err := NodeCreate(context.Background(), nil, NodeUpsertRequest{RawURI: raw, Name: "override"})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	if node.Name != "override" {
		t.Fatalf("Name = %q, want override", node.Name)
	}
	if node.Protocol != ProtocolVLESS {
		t.Fatalf("Protocol = %q, want %q", node.Protocol, ProtocolVLESS)
	}
	if node.RawURI != raw {
		t.Fatalf("RawURI = %q, want original raw URI", node.RawURI)
	}
}

func TestNodeImportExpandsBase64Subscription(t *testing.T) {
	initProxyInMemoryDB(t)

	rawNode := "trojan://password@example.com:443#edge"
	subscription := base64.StdEncoding.EncodeToString([]byte(rawNode + "\n"))

	result, err := NodeImport(context.Background(), nil, NodeImportRequest{Raw: subscription})
	if err != nil {
		t.Fatalf("NodeImport() error = %v", err)
	}
	if result.Imported != 1 || result.Failed != 0 {
		t.Fatalf("NodeImport() result = %+v, want 1 imported and 0 failed", result)
	}
	if result.Items[0].Protocol != ProtocolTrojan {
		t.Fatalf("Protocol = %q, want %q", result.Items[0].Protocol, ProtocolTrojan)
	}
}

func TestMappingCreateAssignsAscendingOrderAndListsOldestFirst(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10081,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate(first) error = %v", err)
	}
	second, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10082,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate(second) error = %v", err)
	}

	if first.Order != 1 || second.Order != 2 {
		t.Fatalf("orders = (%d, %d), want (1, 2)", first.Order, second.Order)
	}

	mappings, err := MappingList(ctx, nil)
	if err != nil {
		t.Fatalf("MappingList() error = %v", err)
	}
	if len(mappings) != 2 {
		t.Fatalf("MappingList() length = %d, want 2", len(mappings))
	}
	if mappings[0].ID != first.ID || mappings[1].ID != second.ID {
		t.Fatalf("MappingList() IDs = (%q, %q), want (%q, %q)", mappings[0].ID, mappings[1].ID, first.ID, second.ID)
	}
}
