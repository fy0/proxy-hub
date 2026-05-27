package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"

	"gorm.io/gorm/logger"
)

func initProxyInMemoryDB(t *testing.T) {
	t.Helper()

	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(HealthStop)
}

func assertUnscopedRowCount(t *testing.T, table any, query string, value any, want int64) {
	t.Helper()

	var count int64
	if err := model.GetTx(nil).Unscoped().Model(table).Where(query, value).Count(&count).Error; err != nil {
		t.Fatalf("count %T error = %v", table, err)
	}
	if count != want {
		t.Fatalf("count %T = %d, want %d", table, count, want)
	}
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

func TestNodeDeleteHardDeletesNodeHealthRows(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "edge",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	if _, err := recordNodeHealthResult(ctx, nil, node.ID, nodeHealthResultRecord{
		Source:    nodeHealthSourceNodeTest,
		TargetID:  node.ID,
		ProbeURL:  "https://example.com/generate_204",
		Available: true,
		LatencyMs: 42,
		CheckedAt: time.Now(),
	}); err != nil {
		t.Fatalf("recordNodeHealthResult() error = %v", err)
	}
	if err := flushNodeHealthBatcher(ctx); err != nil {
		t.Fatalf("flushNodeHealthBatcher() error = %v", err)
	}
	if err := NodeDelete(ctx, nil, node.ID); err != nil {
		t.Fatalf("NodeDelete() error = %v", err)
	}

	assertUnscopedRowCount(t, &tables.ProxyNodeTable{}, "id = ?", node.ID, 0)
	assertUnscopedRowCount(t, &tables.ProxyNodeHealthTable{}, "node_id = ?", node.ID, 0)
	assertUnscopedRowCount(t, &tables.ProxyNodeHealthHistoryTable{}, "node_id = ?", node.ID, 0)
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

func TestNodeImportExpandsClashYAMLWithAdditionalProtocols(t *testing.T) {
	initProxyInMemoryDB(t)

	raw := `proxies:
  - name: ss
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-128-gcm
    password: secret
  - name: hy
    type: hysteria
    server: hy.example.com
    port: 443
    auth-str: auth
    up-mbps: 50
    down-mbps: 100
    sni: edge.example.com
  - name: hy2
    type: hysteria2
    server: hy2.example.com
    port: 443
    password: pass
    obfs-opts:
      type: salamander
      password: obfs
  - name: tuic
    type: tuic
    server: tuic.example.com
    port: 443
    uuid: 48a25c54-8826-4657-330e-8db38ef76716
    password: pass
    congestion-control: bbr
    udp-relay-mode: native
  - name: ssh
    type: ssh
    server: ssh.example.com
    port: 22
    user: root
    password: admin
`

	result, err := NodeImport(context.Background(), nil, NodeImportRequest{Raw: raw})
	if err != nil {
		t.Fatalf("NodeImport() error = %v", err)
	}
	if result.Imported != 5 || result.Failed != 0 {
		t.Fatalf("NodeImport() result = %+v, want 5 imported and 0 failed", result)
	}

	wantTypes := map[string]string{
		ProtocolShadowsocks: constant.TypeShadowsocks,
		ProtocolHysteria:    constant.TypeHysteria,
		ProtocolHysteria2:   constant.TypeHysteria2,
		ProtocolTUIC:        constant.TypeTUIC,
		ProtocolSSH:         constant.TypeSSH,
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		wantType := wantTypes[item.Protocol]
		if wantType == "" {
			t.Fatalf("unexpected protocol in item %+v", item)
		}
		outbound, err := buildNodeOutboundFromURI(item.RawURI, "node-test")
		if err != nil {
			t.Fatalf("buildNodeOutboundFromURI(%s) error = %v", item.Protocol, err)
		}
		if outbound.Type != wantType {
			t.Fatalf("outbound type for %s = %q, want %q", item.Protocol, outbound.Type, wantType)
		}
		seen[item.Protocol] = true
	}
	for protocol := range wantTypes {
		if !seen[protocol] {
			t.Fatalf("protocol %q not imported; items = %+v", protocol, result.Items)
		}
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

func TestGroupCreateAddsNodeMembership(t *testing.T) {
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

func TestGroupCreatePreservesLoadBalanceStrategy(t *testing.T) {
	initProxyInMemoryDB(t)

	group, err := GroupCreate(context.Background(), nil, GroupUpsertRequest{
		Name:     "balanced",
		Strategy: GroupStrategyLoadBalance,
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	if group.Strategy != GroupStrategyLoadBalance {
		t.Fatalf("group strategy = %q, want %q", group.Strategy, GroupStrategyLoadBalance)
	}
}

func TestNodeCreateAcceptsChainMembersWithGroup(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "jump-a",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	groupNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "group-node",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1082),
	})
	if err != nil {
		t.Fatalf("NodeCreate(group-node) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:    "egress",
		NodeIDs: []string{groupNode.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "exit-b",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.3",
		Port:     uint16Ptr(1083),
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}

	chain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "A to group to B",
		Protocol: ProtocolChain,
		ChainMembers: []ChainMemberDTO{
			{Type: ChainMemberTypeNode, ID: first.ID},
			{Type: ChainMemberTypeGroup, ID: group.ID},
			{Type: ChainMemberTypeNode, ID: second.ID},
		},
	})
	if err != nil {
		t.Fatalf("NodeCreate(chain) error = %v", err)
	}

	dto := ToNodeDTO(chain)
	if got, want := dto.ChainNodeIDs, []string{first.ID, second.ID}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("legacy chain node IDs = %v, want %v", got, want)
	}
	if got := dto.ChainMembers; len(got) != 3 || got[1].Type != ChainMemberTypeGroup || got[1].ID != group.ID {
		t.Fatalf("chain members = %+v, want group member in order", got)
	}
	if err := GroupDelete(ctx, nil, group.ID); !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("GroupDelete(referenced) error = %v, want %v", err, ErrInvalidChain)
	}
}

func TestNodeCreateRejectsChainMemberGroupContainingChainNode(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "first",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "second",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1082),
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	innerChain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "inner chain",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeCreate(inner chain) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:    "contains chain",
		NodeIDs: []string{innerChain.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}

	_, err = NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "outer chain",
		Protocol: ProtocolChain,
		ChainMembers: []ChainMemberDTO{
			{Type: ChainMemberTypeNode, ID: first.ID},
			{Type: ChainMemberTypeGroup, ID: group.ID},
		},
	})
	if !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("NodeCreate(outer chain) error = %v, want %v", err, ErrInvalidChain)
	}
}

func TestNodeCreateRejectsNestedChainMemberGroupContainingChainNode(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "first",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "second",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1082),
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	innerChain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "inner chain",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeCreate(inner chain) error = %v", err)
	}
	child := &tables.ProxyGroupTable{
		Name:            "child",
		Type:            GroupTypeSubscription,
		Strategy:        GroupStrategySelector,
		NodeIDsJSON:     encodeStringSlice([]string{innerChain.ID}),
		GroupIDsJSON:    encodeStringSlice(nil),
		BuiltinTagsJSON: encodeStringSlice(nil),
	}
	if err := model.GetDB().WithContext(ctx).Create(child).Error; err != nil {
		t.Fatalf("Create(child) error = %v", err)
	}
	parent := &tables.ProxyGroupTable{
		Name:            "parent",
		Type:            GroupTypeSubscription,
		Strategy:        GroupStrategySelector,
		NodeIDsJSON:     encodeStringSlice(nil),
		GroupIDsJSON:    encodeStringSlice([]string{child.ID}),
		BuiltinTagsJSON: encodeStringSlice(nil),
	}
	if err := model.GetDB().WithContext(ctx).Create(parent).Error; err != nil {
		t.Fatalf("Create(parent) error = %v", err)
	}

	_, err = NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "outer chain",
		Protocol: ProtocolChain,
		ChainMembers: []ChainMemberDTO{
			{Type: ChainMemberTypeNode, ID: first.ID},
			{Type: ChainMemberTypeGroup, ID: parent.ID},
		},
	})
	if !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("NodeCreate(outer chain) error = %v, want %v", err, ErrInvalidChain)
	}
}

func TestGroupUpdateRejectsAddingChainNodeToChainMemberGroup(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "first",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "second",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1082),
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	groupNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "group node",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.3",
		Port:     uint16Ptr(1083),
	})
	if err != nil {
		t.Fatalf("NodeCreate(group node) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "egress", NodeIDs: []string{groupNode.ID}})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	if _, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "outer chain",
		Protocol: ProtocolChain,
		ChainMembers: []ChainMemberDTO{
			{Type: ChainMemberTypeNode, ID: first.ID},
			{Type: ChainMemberTypeGroup, ID: group.ID},
		},
	}); err != nil {
		t.Fatalf("NodeCreate(outer chain) error = %v", err)
	}
	innerChain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "inner chain",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeCreate(inner chain) error = %v", err)
	}

	_, err = GroupUpdate(ctx, nil, group.ID, GroupUpsertRequest{
		Name:    group.Name,
		NodeIDs: []string{groupNode.ID, innerChain.ID},
	})
	if !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("GroupUpdate(add chain) error = %v, want %v", err, ErrInvalidChain)
	}
}

func TestMappingGroupCanContainChainNodeOutsideChainMember(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "first",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "second",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1082),
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	chain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "chain",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeCreate(chain) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "route group", NodeIDs: []string{chain.ID}})
	if err != nil {
		t.Fatalf("GroupCreate(route group) error = %v", err)
	}
	_, err = MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10094,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
}

func TestGroupUpdateClearsNestedGroupReferences(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "first"})
	if err != nil {
		t.Fatalf("GroupCreate(first) error = %v", err)
	}
	second, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "second"})
	if err != nil {
		t.Fatalf("GroupCreate(second) error = %v", err)
	}
	if err := model.GetDB().WithContext(ctx).Model(first).Updates(map[string]any{
		"group_ids_json": encodeStringSlice([]string{second.ID}),
	}).Error; err != nil {
		t.Fatalf("seed nested group refs error = %v", err)
	}

	updated, err := GroupUpdate(ctx, nil, first.ID, GroupUpsertRequest{
		Name:     "first",
		Strategy: GroupStrategySelector,
	})
	if err != nil {
		t.Fatalf("GroupUpdate() error = %v", err)
	}
	if got := decodeStringSlice(updated.GroupIDsJSON); len(got) != 0 {
		t.Fatalf("updated group IDs = %v, want empty", got)
	}
}

func TestNodeUpdateCanAssignMultipleGroups(t *testing.T) {
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
	first, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "first"})
	if err != nil {
		t.Fatalf("GroupCreate(first) error = %v", err)
	}
	second, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "second"})
	if err != nil {
		t.Fatalf("GroupCreate(second) error = %v", err)
	}

	updated, err := NodeUpdate(ctx, nil, node.ID, NodeUpsertRequest{
		Name:     node.Name,
		Protocol: node.Protocol,
		Server:   node.Server,
		Port:     node.Port,
		GroupIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeUpdate() error = %v", err)
	}
	if updated.GroupID != first.ID {
		t.Fatalf("primary group ID = %q, want %q", updated.GroupID, first.ID)
	}

	for _, group := range []*tables.ProxyGroupTable{first, second} {
		refreshed, err := GroupGet(ctx, nil, group.ID)
		if err != nil {
			t.Fatalf("GroupGet(%s) error = %v", group.Name, err)
		}
		if !containsString(decodeStringSlice(refreshed.NodeIDsJSON), node.ID) {
			t.Fatalf("%s node IDs = %v, want node %q", group.Name, decodeStringSlice(refreshed.NodeIDsJSON), node.ID)
		}
	}

	updated, err = NodeUpdate(ctx, nil, node.ID, NodeUpsertRequest{
		Name:     node.Name,
		Protocol: node.Protocol,
		Server:   node.Server,
		Port:     node.Port,
		GroupIDs: []string{second.ID},
	})
	if err != nil {
		t.Fatalf("NodeUpdate(second only) error = %v", err)
	}
	if updated.GroupID != second.ID {
		t.Fatalf("primary group ID after update = %q, want %q", updated.GroupID, second.ID)
	}

	refreshedFirst, err := GroupGet(ctx, nil, first.ID)
	if err != nil {
		t.Fatalf("GroupGet(first) error = %v", err)
	}
	if containsString(decodeStringSlice(refreshedFirst.NodeIDsJSON), node.ID) {
		t.Fatalf("first node IDs = %v, want node removed", decodeStringSlice(refreshedFirst.NodeIDsJSON))
	}
	refreshedSecond, err := GroupGet(ctx, nil, second.ID)
	if err != nil {
		t.Fatalf("GroupGet(second) error = %v", err)
	}
	if !containsString(decodeStringSlice(refreshedSecond.NodeIDsJSON), node.ID) {
		t.Fatalf("second node IDs = %v, want node %q", decodeStringSlice(refreshedSecond.NodeIDsJSON), node.ID)
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
	if result.Imported != 2 || result.Skipped != 0 || len(result.Items) != 0 || len(result.Groups) != 0 {
		t.Fatalf("SubscriptionSync() result = %+v, want compact response for 2 imported nodes", result)
	}
	if subscription.GroupID == "" {
		t.Fatalf("subscription group ID is empty")
	}

	nodes, err := NodeList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("NodeList() length = %d, want 2", len(nodes))
	}
	for _, node := range nodes {
		if node.SubscriptionID == subscription.ID && node.GroupID != subscription.GroupID {
			t.Fatalf("synced node group ID = %q, want %q", node.GroupID, subscription.GroupID)
		}
	}

	groups, err := GroupList(ctx, nil)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	var allGroup *tables.ProxyGroupTable
	var autoGroup *tables.ProxyGroupTable
	for _, group := range groups {
		if group.Name == "all" {
			allGroup = group
		}
		if group.Name == "auto" {
			autoGroup = group
		}
	}
	if allGroup == nil {
		t.Fatalf("all group not found in %+v", groups)
	}
	if autoGroup == nil {
		t.Fatalf("auto group not found in %+v", groups)
	}
	if autoGroup.Strategy != GroupStrategyLeastLatency {
		t.Fatalf("auto group strategy = %q, want %q", autoGroup.Strategy, GroupStrategyLeastLatency)
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
	assertUnscopedRowCount(t, &tables.ProxyNodeTable{}, "name = ?", "us", 0)
	assertUnscopedRowCount(t, &tables.ProxyGroupTable{}, "name = ?", "all", 0)
}

func TestSubscriptionSyncSkipsRulesetPolicyGroupAndGroupOnlyDirect(t *testing.T) {
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
  - name: route-only
    type: select
    proxies:
      - all
      - DIRECT
  - name: ruleset-target
    type: select
    proxies:
      - hk
      - DIRECT
rules:
  - RULE-SET,private,ruleset-target
  - MATCH,all
`
	result, err := SubscriptionSync(ctx, nil, subscription.ID, SubscriptionSyncRequest{Raw: raw})
	if err != nil {
		t.Fatalf("SubscriptionSync() error = %v", err)
	}
	if result.Imported != 2 || result.Skipped != 2 || len(result.Groups) != 0 || len(result.PreviewItems) != 0 {
		t.Fatalf("SubscriptionSync() result = %+v, want 2 nodes and 2 kept groups", result)
	}

	groups, err := GroupList(ctx, nil)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	var allGroup, routeOnlyGroup, rulesetGroup *tables.ProxyGroupTable
	for _, group := range groups {
		switch group.Name {
		case "all":
			allGroup = group
		case "route-only":
			routeOnlyGroup = group
		case "ruleset-target":
			rulesetGroup = group
		}
	}
	if allGroup == nil || routeOnlyGroup == nil {
		t.Fatalf("groups = %+v, want all and route-only", groups)
	}
	if rulesetGroup != nil {
		t.Fatalf("ruleset group was imported: %+v", rulesetGroup)
	}
	if !containsString(decodeStringSlice(allGroup.BuiltinTagsJSON), constantDirect) {
		t.Fatalf("all group builtins = %v, want DIRECT retained", decodeStringSlice(allGroup.BuiltinTagsJSON))
	}
	if containsString(decodeStringSlice(routeOnlyGroup.BuiltinTagsJSON), constantDirect) {
		t.Fatalf("route-only builtins = %v, want DIRECT removed", decodeStringSlice(routeOnlyGroup.BuiltinTagsJSON))
	}
}

func TestNodeImportClashYAMLImportsManualGroups(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	raw := `proxies:
  - name: hk
    type: vless
    server: hk.example.com
    port: 443
    uuid: 48a25c54-8826-4657-330e-8db38ef76716
    tls: true
proxy-groups:
  - name: all
    type: select
    proxies:
      - hk
      - DIRECT
`
	preview, err := NodeImportPreview(ctx, nil, NodeImportRequest{Raw: raw})
	if err != nil {
		t.Fatalf("NodeImportPreview() error = %v", err)
	}
	if !previewContains(preview.PreviewItems, "all", ImportPreviewActionImport, ImportPreviewReasonImport) {
		t.Fatalf("preview items = %+v, want group import preview", preview.PreviewItems)
	}

	result, err := NodeImport(ctx, nil, NodeImportRequest{Raw: raw})
	if err != nil {
		t.Fatalf("NodeImport() error = %v", err)
	}
	if len(result.Items) != 1 || len(result.Groups) != 1 {
		t.Fatalf("NodeImport() result = %+v, want node and group", result)
	}
	groups, err := GroupList(ctx, nil)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	var imported *tables.ProxyGroupTable
	for _, group := range groups {
		if group.Name == "all" {
			imported = group
			break
		}
	}
	if imported == nil {
		t.Fatalf("imported group not found in %+v", groups)
	}
	if imported.Type != GroupTypeManual {
		t.Fatalf("imported group type = %q, want manual", imported.Type)
	}
	if len(decodeStringSlice(imported.NodeIDsJSON)) != 1 {
		t.Fatalf("imported group node IDs = %v, want one node", decodeStringSlice(imported.NodeIDsJSON))
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
	if result.Imported != 1 {
		t.Fatalf("SubscriptionSync() result = %+v, want one imported node", result)
	}
	groups, err := GroupList(ctx, nil)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	var importedGroup *tables.ProxyGroupTable
	for _, group := range groups {
		if group.Name == "auto" {
			importedGroup = group
			break
		}
	}
	if importedGroup == nil {
		t.Fatalf("groups = %+v, want imported auto group", groups)
	}

	_, err = SubscriptionCreate(ctx, nil, SubscriptionUpsertRequest{
		Name:    "target",
		URL:     "https://example.com/target.yaml",
		GroupID: importedGroup.ID,
	})
	if err != ErrInvalidGroup {
		t.Fatalf("SubscriptionCreate(imported group) error = %v, want %v", err, ErrInvalidGroup)
	}
}

func TestSubscriptionSyncLargeClashSubscriptionReturnsCompactResult(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	subscription, err := SubscriptionCreate(ctx, nil, SubscriptionUpsertRequest{
		Name: "large",
		URL:  "https://example.com/large.yaml",
	})
	if err != nil {
		t.Fatalf("SubscriptionCreate() error = %v", err)
	}

	raw := largeClashSubscriptionRaw(1200)
	result, err := SubscriptionSync(ctx, nil, subscription.ID, SubscriptionSyncRequest{Raw: raw})
	if err != nil {
		t.Fatalf("SubscriptionSync() error = %v", err)
	}
	if result.Imported != 1200 || result.Failed != 0 || result.Total != 1200 {
		t.Fatalf("SubscriptionSync() result = %+v, want 1200 imported", result)
	}
	if len(result.Items) != 0 || len(result.Groups) != 0 || len(result.PreviewItems) != 0 {
		t.Fatalf("SubscriptionSync() detail lengths = items %d groups %d preview %d, want compact response",
			len(result.Items),
			len(result.Groups),
			len(result.PreviewItems),
		)
	}

	nodes, err := NodeList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	if len(nodes) != 1200 {
		t.Fatalf("NodeList() length = %d, want 1200", len(nodes))
	}
	rootGroup, err := GroupGet(ctx, nil, subscription.GroupID)
	if err != nil {
		t.Fatalf("GroupGet(root) error = %v", err)
	}
	if len(decodeStringSlice(rootGroup.NodeIDsJSON)) != 1200 {
		t.Fatalf("root group node IDs = %d, want 1200", len(decodeStringSlice(rootGroup.NodeIDsJSON)))
	}

	result, err = SubscriptionSync(ctx, nil, subscription.ID, SubscriptionSyncRequest{Raw: raw})
	if err != nil {
		t.Fatalf("SubscriptionSync(repeat) error = %v", err)
	}
	if result.Updated != 1200 || result.Imported != 0 {
		t.Fatalf("SubscriptionSync(repeat) result = %+v, want 1200 updated and 0 imported", result)
	}
	nodes, err = NodeList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeList(repeat) error = %v", err)
	}
	if len(nodes) != 1200 {
		t.Fatalf("NodeList(repeat) length = %d, want no duplicates", len(nodes))
	}
}

func TestHealthStartDoesNotProbeAllNodes(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(HealthStop)

	ctx := context.Background()
	port := uint16(1080)
	for i := 0; i < 5; i++ {
		if _, err := NodeCreate(ctx, nil, NodeUpsertRequest{
			Name:     fmt.Sprintf("node-%d", i),
			Protocol: ProtocolSOCKS5,
			Server:   "127.0.0.1",
			Port:     &port,
		}); err != nil {
			t.Fatalf("NodeCreate(%d) error = %v", i, err)
		}
	}

	HealthStart(ctx, utils.ProxyHealthConfig{
		Enabled:        true,
		ProbeURL:       "http://127.0.0.1/",
		Interval:       time.Millisecond,
		Timeout:        time.Millisecond,
		MaxConcurrency: 1,
	})
	time.Sleep(20 * time.Millisecond)

	rows, err := NodeHealthList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeHealthList() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("NodeHealthList() length = %d, want no automatic startup probes", len(rows))
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

func TestMappingCreateAcceptsLeastLatencyStrategy(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10083,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyLeastLatency,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if mapping.Strategy != StrategyLeastLatency {
		t.Fatalf("mapping strategy = %q, want %q", mapping.Strategy, StrategyLeastLatency)
	}
}

func TestMappingUpsertClearsActiveRouteForNonManualStrategy(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10087,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyLeastLatency,
		NodeIDs:          []string{node.ID},
		ActiveNodeID:     &node.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if mapping.ActiveNodeID != "" || mapping.ActiveGroupID != "" {
		t.Fatalf("active route = node %q group %q, want empty for non-manual strategy", mapping.ActiveNodeID, mapping.ActiveGroupID)
	}

	updated, err := MappingUpdate(ctx, nil, mapping.ID, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Strategy:         StrategyLoadBalance,
		NodeIDs:          []string{node.ID},
		ActiveNodeID:     &node.ID,
	})
	if err != nil {
		t.Fatalf("MappingUpdate() error = %v", err)
	}
	if updated.ActiveNodeID != "" || updated.ActiveGroupID != "" {
		t.Fatalf("updated active route = node %q group %q, want empty for non-manual strategy", updated.ActiveNodeID, updated.ActiveGroupID)
	}
}

func TestMappingCreateDefaultsToLeastLatencyStrategy(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10084,
		OutboundProtocol: OutboundProtocolMixed,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if mapping.Strategy != StrategyLeastLatency {
		t.Fatalf("mapping strategy = %q, want %q", mapping.Strategy, StrategyLeastLatency)
	}
}

func TestMappingGroupStrategyOverridesAreNormalized(t *testing.T) {
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
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:    "auto",
		NodeIDs: []string{node.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10091,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyLeastLatency,
		GroupIDs:         []string{group.ID},
		GroupStrategyOverrides: map[string]string{
			group.ID: GroupStrategyOverrideLoadBalance,
		},
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if got := decodeGroupStrategyOverrides(mapping.GroupStrategyOverridesJSON)[group.ID]; got != GroupStrategyOverrideLoadBalance {
		t.Fatalf("group strategy override = %q, want %q", got, GroupStrategyOverrideLoadBalance)
	}
	if dto := ToMappingDTO(mapping); dto.GroupStrategyOverrides[group.ID] != GroupStrategyOverrideLoadBalance {
		t.Fatalf("dto overrides = %+v, want load-balance override", dto.GroupStrategyOverrides)
	}

	updated, err := MappingUpdate(ctx, nil, mapping.ID, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Strategy:         mapping.Strategy,
		GroupIDs:         []string{group.ID},
		GroupStrategyOverrides: map[string]string{
			group.ID: GroupStrategyOverrideInherit,
		},
	})
	if err != nil {
		t.Fatalf("MappingUpdate() error = %v", err)
	}
	if got := decodeGroupStrategyOverrides(updated.GroupStrategyOverridesJSON); len(got) != 0 {
		t.Fatalf("group strategy overrides = %+v, want empty after inherit", got)
	}
}

func TestMappingGroupStrategyOverridesRejectInvalidGroupOrStrategy(t *testing.T) {
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
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "auto", NodeIDs: []string{node.ID}})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	base := MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10092,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyLeastLatency,
		GroupIDs:         []string{group.ID},
	}
	invalidGroup := base
	invalidGroup.GroupStrategyOverrides = map[string]string{"missing": GroupStrategyOverrideLoadBalance}
	if _, err := MappingCreate(ctx, nil, invalidGroup); !errors.Is(err, ErrInvalidMapping) {
		t.Fatalf("MappingCreate(invalid group override) error = %v, want %v", err, ErrInvalidMapping)
	}
	invalidStrategy := base
	invalidStrategy.GroupStrategyOverrides = map[string]string{group.ID: "selector"}
	if _, err := MappingCreate(ctx, nil, invalidStrategy); !errors.Is(err, ErrInvalidMapping) {
		t.Fatalf("MappingCreate(invalid strategy override) error = %v, want %v", err, ErrInvalidMapping)
	}
}

func TestMappingSwitchUpdatesActiveRoute(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node) error = %v", err)
	}
	groupNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "group-node",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(group-node) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:    "group",
		NodeIDs: []string{groupNode.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10084,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{node.ID},
		GroupIDs:         []string{group.ID},
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	switched, err := MappingSwitch(ctx, nil, mapping.ID, MappingSwitchRequest{
		TargetType: MappingSwitchTargetNode,
		TargetID:   node.ID,
	})
	if err != nil {
		t.Fatalf("MappingSwitch(node) error = %v", err)
	}
	if switched.ActiveNodeID != node.ID || switched.ActiveGroupID != "" {
		t.Fatalf("active after node switch = node %q group %q, want node only", switched.ActiveNodeID, switched.ActiveGroupID)
	}

	switched, err = MappingSwitch(ctx, nil, mapping.ID, MappingSwitchRequest{
		TargetType: MappingSwitchTargetGroup,
		TargetID:   group.ID,
	})
	if err != nil {
		t.Fatalf("MappingSwitch(group) error = %v", err)
	}
	if switched.ActiveNodeID != "" || switched.ActiveGroupID != group.ID {
		t.Fatalf("active after group switch = node %q group %q, want group only", switched.ActiveNodeID, switched.ActiveGroupID)
	}
}

func TestMappingSwitchRejectsInvalidTargets(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node) error = %v", err)
	}
	other, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "other",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(other) error = %v", err)
	}
	manual, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10085,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{node.ID},
	})
	if err != nil {
		t.Fatalf("MappingCreate(manual) error = %v", err)
	}
	if _, err := MappingSwitch(ctx, nil, manual.ID, MappingSwitchRequest{TargetType: MappingSwitchTargetNode, TargetID: other.ID}); !errors.Is(err, ErrInvalidMappingSwitch) {
		t.Fatalf("MappingSwitch(non-member) error = %v, want %v", err, ErrInvalidMappingSwitch)
	}

	loadBalanced, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10086,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyLoadBalance,
		NodeIDs:          []string{node.ID},
	})
	if err != nil {
		t.Fatalf("MappingCreate(load-balanced) error = %v", err)
	}
	if _, err := MappingSwitch(ctx, nil, loadBalanced.ID, MappingSwitchRequest{TargetType: MappingSwitchTargetNode, TargetID: node.ID}); !errors.Is(err, ErrInvalidMappingSwitch) {
		t.Fatalf("MappingSwitch(non-manual) error = %v, want %v", err, ErrInvalidMappingSwitch)
	}
}

func TestSettingsImportRoundTripReplacesExistingConfig(t *testing.T) {
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
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "manual", NodeIDs: []string{node.ID}})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	_, err = MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10090,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{node.ID},
		ActiveNodeID:     &node.ID,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	backup, err := SettingsExport(ctx, nil)
	if err != nil {
		t.Fatalf("SettingsExport() error = %v", err)
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

	result, err := SettingsImport(ctx, *backup)
	if err != nil {
		t.Fatalf("SettingsImport() error = %v", err)
	}
	if result.Nodes != 1 || result.Groups != 1 || result.Mappings != 1 {
		t.Fatalf("SettingsImport() result = %+v, want one node/group/mapping", result)
	}

	nodes, err := NodeList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != node.ID || nodes[0].Password != "pass" {
		t.Fatalf("nodes after import = %+v, want original node only", nodes)
	}
	if _, err := NodeGet(ctx, nil, extra.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("NodeGet(extra) error = %v, want %v", err, ErrNodeNotFound)
	}

	mappings, err := MappingList(ctx, nil)
	if err != nil {
		t.Fatalf("MappingList() error = %v", err)
	}
	if len(mappings) != 1 ||
		!containsString(decodeStringSlice(mappings[0].NodeIDsJSON), node.ID) ||
		!containsString(decodeStringSlice(mappings[0].GroupIDsJSON), group.ID) {
		t.Fatalf("mappings after import = %+v, want restored references", mappings)
	}
}

func TestSettingsImportRejectsBrokenReferenceWithoutMutation(t *testing.T) {
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

	backup := SettingsBackupDTO{
		Kind:          SettingsBackupKind,
		SchemaVersion: SettingsBackupSchemaVersion,
		ExportedAt:    original.CreatedAt,
		Data: SettingsBackupDataDTO{
			Groups: []*ProxyGroupDTO{{
				ID:       "group-1",
				Name:     "broken",
				Type:     GroupTypeManual,
				Strategy: GroupStrategySelector,
				NodeIDs:  []string{"missing-node"},
			}},
		},
	}

	_, err = SettingsImport(ctx, backup)
	if !errors.Is(err, ErrInvalidSettingsBackup) {
		t.Fatalf("SettingsImport() error = %v, want %v", err, ErrInvalidSettingsBackup)
	}

	nodes, err := NodeList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != original.ID {
		t.Fatalf("nodes after rejected import = %+v, want original data unchanged", nodes)
	}
}

func TestSettingsImportRejectsChainMemberGroupContainingChainNode(t *testing.T) {
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
		t.Fatalf("NodeCreate(original) error = %v", err)
	}
	firstPort := uint16(1081)
	secondPort := uint16(1082)
	backup := SettingsBackupDTO{
		Kind:          SettingsBackupKind,
		SchemaVersion: SettingsBackupSchemaVersion,
		ExportedAt:    original.CreatedAt,
		Data: SettingsBackupDataDTO{
			Nodes: []*ProxyNodeDTO{
				{
					ID:       "node-first",
					Name:     "first",
					Protocol: ProtocolSOCKS5,
					Server:   "127.0.0.1",
					Port:     &firstPort,
				},
				{
					ID:       "node-second",
					Name:     "second",
					Protocol: ProtocolHTTP,
					Server:   "127.0.0.2",
					Port:     &secondPort,
				},
				{
					ID:           "chain-inner",
					Name:         "inner",
					Protocol:     ProtocolChain,
					ChainNodeIDs: []string{"node-first", "node-second"},
				},
				{
					ID:       "chain-outer",
					Name:     "outer",
					Protocol: ProtocolChain,
					ChainMembers: []ChainMemberDTO{
						{Type: ChainMemberTypeNode, ID: "node-first"},
						{Type: ChainMemberTypeGroup, ID: "group-parent"},
					},
				},
			},
			Groups: []*ProxyGroupDTO{
				{
					ID:       "group-child",
					Name:     "child",
					Type:     GroupTypeSubscription,
					Strategy: GroupStrategySelector,
					NodeIDs:  []string{"chain-inner"},
				},
				{
					ID:       "group-parent",
					Name:     "parent",
					Type:     GroupTypeSubscription,
					Strategy: GroupStrategySelector,
					GroupIDs: []string{"group-child"},
				},
			},
		},
	}

	_, err = SettingsImport(ctx, backup)
	if !errors.Is(err, ErrInvalidSettingsBackup) {
		t.Fatalf("SettingsImport() error = %v, want %v", err, ErrInvalidSettingsBackup)
	}

	nodes, err := NodeList(ctx, nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != original.ID {
		t.Fatalf("nodes after rejected import = %+v, want original data unchanged", nodes)
	}
}

func uint16Ptr(value uint16) *uint16 {
	return &value
}

func largeClashSubscriptionRaw(count int) string {
	var builder strings.Builder
	builder.WriteString("proxies:\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&builder, "  - name: node-%04d\n", i)
		builder.WriteString("    type: trojan\n")
		fmt.Fprintf(&builder, "    server: node-%04d.example.com\n", i)
		builder.WriteString("    port: 443\n")
		builder.WriteString("    password: secret\n")
	}
	return builder.String()
}

func previewContains(items []NodeImportPreviewItem, name, action, reason string) bool {
	for _, item := range items {
		if item.Name == name && item.Action == action && item.Reason == reason {
			return true
		}
	}
	return false
}
