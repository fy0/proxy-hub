package proxy

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"proxy-hub/model"
	"proxy-hub/model/tables"

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

func TestNodeImportExpandsClashYAML(t *testing.T) {
	initProxyInMemoryDB(t)

	raw := `proxies:
  - name: vless h2
    type: vless
    server: h2.example.com
    port: 443
    uuid: 48a25c54-8826-4657-330e-8db38ef76716
    tls: true
    network: h2
    servername: edge.example.com
    h2-opts:
      host:
        - cdn.example.com
      path: /h2
  - name: trojan ws
    type: trojan
    server: trojan.example.com
    port: 443
    password: secret
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
`

	result, err := NodeImport(context.Background(), nil, NodeImportRequest{Raw: raw})
	if err != nil {
		t.Fatalf("NodeImport() error = %v", err)
	}
	if result.Imported != 2 || result.Failed != 0 {
		t.Fatalf("NodeImport() result = %+v, want 2 imported and 0 failed", result)
	}
	if result.Items[0].Protocol != ProtocolVLESS || !containsString(result.Items[0].Tags, "h2") {
		t.Fatalf("first item = %+v, want vless h2", result.Items[0])
	}

	outbound, err := buildNodeOutboundFromURI(result.Items[0].RawURI, "node-test")
	if err != nil {
		t.Fatalf("buildNodeOutboundFromURI() error = %v", err)
	}
	options, ok := outbound.Options.(*option.VLESSOutboundOptions)
	if !ok {
		t.Fatalf("Options type = %T, want *option.VLESSOutboundOptions", outbound.Options)
	}
	if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeHTTP {
		t.Fatalf("Transport = %+v, want http transport for Clash h2", options.Transport)
	}
	if options.Transport.HTTPOptions.Path != "/h2" {
		t.Fatalf("HTTP path = %q, want /h2", options.Transport.HTTPOptions.Path)
	}
}

func TestNodeImportFetchesSubscriptionURL(t *testing.T) {
	initProxyInMemoryDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`proxies:
  - name: hk
    type: trojan
    server: hk.example.com
    port: 443
    password: secret
`))
	}))
	t.Cleanup(server.Close)

	result, err := NodeImport(context.Background(), nil, NodeImportRequest{Raw: server.URL + "/sub/mihomo.yaml"})
	if err != nil {
		t.Fatalf("NodeImport() error = %v", err)
	}
	if result.Imported != 1 || result.Failed != 0 {
		t.Fatalf("NodeImport() result = %+v, want fetched subscription node", result)
	}
	if result.Items[0].Name != "hk" {
		t.Fatalf("imported node name = %q, want hk", result.Items[0].Name)
	}
}

func TestNodeImportAssignsGroup(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "manual"})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}

	rawNode := "trojan://password@example.com:443#edge"
	result, err := NodeImport(ctx, nil, NodeImportRequest{Raw: rawNode, GroupID: group.ID})
	if err != nil {
		t.Fatalf("NodeImport() error = %v", err)
	}
	if result.Imported != 1 || result.Items[0].GroupID != group.ID {
		t.Fatalf("NodeImport() result = %+v, want imported node in group %q", result, group.ID)
	}
	refreshed, err := GroupGet(ctx, nil, group.ID)
	if err != nil {
		t.Fatalf("GroupGet() error = %v", err)
	}
	if !containsString(decodeStringSlice(refreshed.NodeIDsJSON), result.Items[0].ID) {
		t.Fatalf("group node IDs = %v, want imported node", decodeStringSlice(refreshed.NodeIDsJSON))
	}
}

func TestGroupCreateAssignsNodeOwnership(t *testing.T) {
	initProxyInMemoryDB(t)

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
	refreshed, err := NodeGet(ctx, nil, node.ID)
	if err != nil {
		t.Fatalf("NodeGet() error = %v", err)
	}
	if refreshed.GroupID != group.ID {
		t.Fatalf("node group ID = %q, want %q", refreshed.GroupID, group.ID)
	}
}

func TestSubscriptionSyncImportsClashGroupsAndReplacesMissingItems(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	subscription, err := SubscriptionCreate(ctx, nil, SubscriptionUpsertRequest{
		Name: "mihomo",
		URL:  "https://example.com/sub.yaml",
	})
	if err != nil {
		t.Fatalf("SubscriptionCreate() error = %v", err)
	}

	raw := `proxies:
  - name: hk
    type: vless
    server: hk.example.com
    port: 443
    uuid: 48a25c54-8826-4657-330e-8db38ef76716
    tls: true
  - name: us
    type: trojan
    server: us.example.com
    port: 443
    password: secret
proxy-groups:
  - name: all
    type: select
    proxies:
      - hk
      - us
      - DIRECT
  - name: auto
    type: url-test
    include-all: true
    filter: hk|us
`
	result, err := SubscriptionSync(ctx, nil, subscription.ID, SubscriptionSyncRequest{Raw: raw})
	if err != nil {
		t.Fatalf("SubscriptionSync() error = %v", err)
	}
	if result.Imported != 2 || len(result.Groups) != 2 || result.Skipped != 0 {
		t.Fatalf("SubscriptionSync() result = %+v, want 2 nodes and 2 groups", result)
	}
	if subscription.GroupID == "" {
		t.Fatalf("subscription group ID is empty")
	}
	for _, item := range result.Items {
		if item.GroupID != subscription.GroupID {
			t.Fatalf("synced node group ID = %q, want %q", item.GroupID, subscription.GroupID)
		}
	}

	groups, err := GroupList(ctx, nil)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	var allGroup *tables.ProxyGroupTable
	for _, group := range groups {
		if group.Name == "all" {
			allGroup = group
			break
		}
	}
	if allGroup == nil {
		t.Fatalf("all group not found in %+v", groups)
	}
	if len(decodeStringSlice(allGroup.NodeIDsJSON)) != 2 {
		t.Fatalf("all group node IDs = %v, want 2", decodeStringSlice(allGroup.NodeIDsJSON))
	}
	if !containsString(decodeStringSlice(allGroup.BuiltinTagsJSON), constantDirect) {
		t.Fatalf("all group builtins = %v, want DIRECT", decodeStringSlice(allGroup.BuiltinTagsJSON))
	}
	rootGroup, err := GroupGet(ctx, nil, subscription.GroupID)
	if err != nil {
		t.Fatalf("GroupGet(root) error = %v", err)
	}
	if len(decodeStringSlice(rootGroup.NodeIDsJSON)) != 2 || len(decodeStringSlice(rootGroup.GroupIDsJSON)) != 2 {
		t.Fatalf("root group refs = nodes %v groups %v, want imported nodes and groups",
			decodeStringSlice(rootGroup.NodeIDsJSON),
			decodeStringSlice(rootGroup.GroupIDsJSON),
		)
	}

	_, err = MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10091,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{allGroup.ID},
		ActiveGroupID:    &allGroup.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	updatedRaw := `proxies:
  - name: hk
    type: vless
    server: hk2.example.com
    port: 443
    uuid: 48a25c54-8826-4657-330e-8db38ef76716
    tls: true
proxy-groups:
  - name: auto
    type: url-test
    include-all: true
`
	result, err = SubscriptionSync(ctx, nil, subscription.ID, SubscriptionSyncRequest{Raw: updatedRaw})
	if err != nil {
		t.Fatalf("SubscriptionSync(update) error = %v", err)
	}
	if result.Updated != 1 || result.Deleted < 2 {
		t.Fatalf("SubscriptionSync(update) result = %+v, want updated node and deleted old node/group", result)
	}
	mappings, err := MappingList(ctx, nil)
	if err != nil {
		t.Fatalf("MappingList() error = %v", err)
	}
	if len(mappings) != 1 || len(decodeStringSlice(mappings[0].GroupIDsJSON)) != 0 || mappings[0].ActiveGroupID != "" {
		t.Fatalf("mapping group refs = %+v, want cleaned deleted group", mappings[0])
	}
}

func TestSubscriptionCreateRejectsImportedGroupTarget(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	source, err := SubscriptionCreate(ctx, nil, SubscriptionUpsertRequest{
		Name: "source",
		URL:  "https://example.com/source.yaml",
	})
	if err != nil {
		t.Fatalf("SubscriptionCreate(source) error = %v", err)
	}
	result, err := SubscriptionSync(ctx, nil, source.ID, SubscriptionSyncRequest{Raw: `proxies:
  - name: hk
    type: trojan
    server: hk.example.com
    port: 443
    password: secret
proxy-groups:
  - name: auto
    type: url-test
    include-all: true
`})
	if err != nil {
		t.Fatalf("SubscriptionSync() error = %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("synced groups = %+v, want one imported group", result.Groups)
	}

	_, err = SubscriptionCreate(ctx, nil, SubscriptionUpsertRequest{
		Name:    "target",
		URL:     "https://example.com/target.yaml",
		GroupID: result.Groups[0].ID,
	})
	if err != ErrInvalidGroup {
		t.Fatalf("SubscriptionCreate(imported group) error = %v, want %v", err, ErrInvalidGroup)
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

func uint16Ptr(value uint16) *uint16 {
	return &value
}
