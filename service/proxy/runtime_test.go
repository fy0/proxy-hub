package proxy

import (
	"context"
	"errors"
	"net"
	"testing"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model"
	"proxy-hub/model/tables"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"gorm.io/gorm/logger"
)

func TestRuntimeReloadStartsEnabledMapping(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	nodePort := uint16(65000)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "local socks",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &nodePort,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}

	listenPort := freeTCPPort(t)
	_, err = MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       listenPort,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{node.ID},
		ActiveNodeID:     &node.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	status, err := RuntimeReload(ctx)
	if err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	if !status.Running {
		t.Fatalf("Runtime status = %+v, want running", status)
	}
	if len(status.Inbounds) != 1 {
		t.Fatalf("Runtime inbounds = %d, want 1", len(status.Inbounds))
	}
}

func TestBuildSingBoxOptionsIncludesMappingWithoutNodes(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	options, inbounds, err := BuildSingBoxOptions(ctx, nil)
	if err != nil {
		t.Fatalf("BuildSingBoxOptions() error = %v", err)
	}
	if len(options.Inbounds) != 1 {
		t.Fatalf("options.Inbounds length = %d, want 1", len(options.Inbounds))
	}
	if len(inbounds) != 1 {
		t.Fatalf("runtime inbounds length = %d, want 1", len(inbounds))
	}
	if inbounds[0].MappingID != mapping.ID {
		t.Fatalf("runtime inbound mapping ID = %q, want %q", inbounds[0].MappingID, mapping.ID)
	}
	if inbounds[0].Outbound != constant.TypeBlock {
		t.Fatalf("runtime inbound outbound = %q, want %q", inbounds[0].Outbound, constant.TypeBlock)
	}
}

func TestBuildSingBoxOptionsRoutesMappingToGroup(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "local socks",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:     "manual group",
		Strategy: GroupStrategySelector,
		NodeIDs:  []string{node.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	options, inbounds, err := BuildSingBoxOptions(ctx, nil)
	if err != nil {
		t.Fatalf("BuildSingBoxOptions() error = %v", err)
	}
	if len(inbounds) != 1 || inbounds[0].MappingID != mapping.ID {
		t.Fatalf("runtime inbounds = %+v, want mapping %q", inbounds, mapping.ID)
	}
	if inbounds[0].Outbound != proxyGroupOutboundTag(group.ID) {
		t.Fatalf("runtime outbound = %q, want group tag", inbounds[0].Outbound)
	}
	hasGroupOutbound := false
	for _, outbound := range options.Outbounds {
		if outbound.Tag == proxyGroupOutboundTag(group.ID) {
			hasGroupOutbound = true
			break
		}
	}
	if !hasGroupOutbound {
		t.Fatalf("outbounds = %+v, want group outbound", options.Outbounds)
	}
}

func TestBuildSingBoxOptionsDoesNotEmitURLTestOutbounds(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "first",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "second",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:     "url test group",
		Strategy: GroupStrategyURLTest,
		NodeIDs:  []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyFailover,
		NodeIDs:          []string{first.ID, second.ID},
		GroupIDs:         []string{group.ID},
		ActiveNodeID:     &first.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	options, _, err := BuildSingBoxOptions(ctx, nil)
	if err != nil {
		t.Fatalf("BuildSingBoxOptions() error = %v", err)
	}

	for _, outbound := range options.Outbounds {
		if outbound.Type == constant.TypeURLTest {
			t.Fatalf("outbound %q type = %q, want no URL test outbounds during runtime load", outbound.Tag, outbound.Type)
		}
	}
	groupOutbound := findTestOutbound(options.Outbounds, proxyGroupOutboundTag(group.ID))
	if groupOutbound == nil || groupOutbound.Type != constant.TypeSelector {
		t.Fatalf("group outbound = %+v, want selector", groupOutbound)
	}
	mappingOutbound := findTestOutbound(options.Outbounds, mappingOutboundTag(mapping.ID))
	if mappingOutbound == nil || mappingOutbound.Type != constant.TypeSelector {
		t.Fatalf("mapping outbound = %+v, want selector", mappingOutbound)
	}
}

func TestLeastLatencyGroupUsesLeastLatencyPolicy(t *testing.T) {
	group := &tables.ProxyGroupTable{Strategy: GroupStrategyLeastLatency}
	policy := policyForGroup(group)
	if policy.Strategy != singboxcore.BalanceLeastLatency {
		t.Fatalf("policy strategy = %q, want %q", policy.Strategy, singboxcore.BalanceLeastLatency)
	}
	if policy.ProbeURL == "" {
		t.Fatalf("policy probe URL is empty")
	}
	if policy.ProbeConcurrency <= 0 {
		t.Fatalf("policy probe concurrency = %d, want positive", policy.ProbeConcurrency)
	}
	if policy.ProbeTimeout <= 0 {
		t.Fatalf("policy probe timeout = %v, want positive", policy.ProbeTimeout)
	}
}

func TestLeastLatencyMappingUsesLeastLatencyPolicy(t *testing.T) {
	mapping := &tables.PortMappingTable{Strategy: StrategyLeastLatency}
	policy := policyForMapping(mapping)
	if policy.Strategy != singboxcore.BalanceLeastLatency {
		t.Fatalf("policy strategy = %q, want %q", policy.Strategy, singboxcore.BalanceLeastLatency)
	}
	if policy.ProbeURL == "" {
		t.Fatalf("policy probe URL is empty")
	}
	if policy.ProbeConcurrency <= 0 {
		t.Fatalf("policy probe concurrency = %d, want positive", policy.ProbeConcurrency)
	}
	if policy.ProbeTimeout <= 0 {
		t.Fatalf("policy probe timeout = %v, want positive", policy.ProbeTimeout)
	}
	if policy.FallbackStrategy != singboxcore.BalanceRoundRobin {
		t.Fatalf("policy fallback strategy = %q, want %q", policy.FallbackStrategy, singboxcore.BalanceRoundRobin)
	}
}

func TestSubscriptionURLTestGroupUsesLeastLatencyPolicy(t *testing.T) {
	group := &tables.ProxyGroupTable{
		Type:     GroupTypeSubscription,
		Strategy: GroupStrategyURLTest,
	}
	policy := policyForGroup(group)
	if policy.Strategy != singboxcore.BalanceLeastLatency {
		t.Fatalf("subscription url-test policy strategy = %q, want %q", policy.Strategy, singboxcore.BalanceLeastLatency)
	}
}

func TestManualURLTestGroupKeepsRoundRobinPolicy(t *testing.T) {
	group := &tables.ProxyGroupTable{
		Type:     GroupTypeManual,
		Strategy: GroupStrategyURLTest,
	}
	policy := policyForGroup(group)
	if policy.Strategy != singboxcore.BalanceRoundRobin {
		t.Fatalf("manual url-test policy strategy = %q, want %q", policy.Strategy, singboxcore.BalanceRoundRobin)
	}
}

func TestBuildSingBoxOptionsRoutesChainNodeWithDetour(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	firstPort := uint16(1081)
	secondPort := uint16(1082)
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "jump-a",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &firstPort,
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "exit-b",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.2",
		Port:     &secondPort,
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	chain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "A to B",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeCreate(chain) error = %v", err)
	}
	if _, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{chain.ID},
		ActiveNodeID:     &chain.ID,
	}); err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	options, inbounds, err := BuildSingBoxOptions(ctx, nil)
	if err != nil {
		t.Fatalf("BuildSingBoxOptions() error = %v", err)
	}
	if len(inbounds) != 1 || inbounds[0].Outbound != nodeOutboundTag(chain.ID) {
		t.Fatalf("runtime inbound outbound = %+v, want chain node tag", inbounds)
	}

	finalOutbound := findTestOutbound(options.Outbounds, nodeOutboundTag(chain.ID))
	if finalOutbound == nil {
		t.Fatalf("chain outbound %q not found", nodeOutboundTag(chain.ID))
	}
	dialer, ok := finalOutbound.Options.(option.DialerOptionsWrapper)
	if !ok {
		t.Fatalf("chain outbound options type = %T, want dialer options", finalOutbound.Options)
	}
	wantDetour := nodeChainMemberOutboundTag(chain.ID, 0, first.ID)
	if got := dialer.TakeDialerOptions().Detour; got != wantDetour {
		t.Fatalf("chain final detour = %q, want %q", got, wantDetour)
	}
	if findTestOutbound(options.Outbounds, wantDetour) == nil {
		t.Fatalf("first chain member outbound %q not found", wantDetour)
	}
}

func TestBuildHealthProbeNodeOutboundsSupportsChainNodes(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	firstPort := uint16(1081)
	secondPort := uint16(1082)
	first, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "jump-a",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &firstPort,
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "exit-b",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.2",
		Port:     &secondPort,
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	chain, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "A to B",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("NodeCreate(chain) error = %v", err)
	}

	tag, outbounds, err := buildHealthProbeNodeOutbounds(ctx, chain)
	if err != nil {
		t.Fatalf("buildHealthProbeNodeOutbounds() error = %v", err)
	}
	if tag != nodeOutboundTag(chain.ID) {
		t.Fatalf("health probe outbound tag = %q, want %q", tag, nodeOutboundTag(chain.ID))
	}
	finalOutbound := findTestOutbound(outbounds, nodeOutboundTag(chain.ID))
	if finalOutbound == nil {
		t.Fatalf("chain outbound %q not found", nodeOutboundTag(chain.ID))
	}
	dialer, ok := finalOutbound.Options.(option.DialerOptionsWrapper)
	if !ok {
		t.Fatalf("chain outbound options type = %T, want dialer options", finalOutbound.Options)
	}
	wantDetour := nodeChainMemberOutboundTag(chain.ID, 0, first.ID)
	if got := dialer.TakeDialerOptions().Detour; got != wantDetour {
		t.Fatalf("health probe chain final detour = %q, want %q", got, wantDetour)
	}
	if findTestOutbound(outbounds, wantDetour) == nil {
		t.Fatalf("first chain member outbound %q not found", wantDetour)
	}
}

func TestBuildSingBoxOptionsIncludesAdditionalProtocolOutbounds(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	rawURIs := []string{
		"ss://aes-128-gcm:secret@ss.example.com:8388#ss",
		"hysteria://auth@hy.example.com:443?upmbps=50&downmbps=100#hy",
		"hy2://pass@hy2.example.com:443#hy2",
		"tuic://48a25c54-8826-4657-330e-8db38ef76716:pass@tuic.example.com:443#tuic",
		"ssh://root:admin@ssh.example.com:22#ssh",
	}
	nodeIDs := make([]string, 0, len(rawURIs))
	for _, rawURI := range rawURIs {
		node, err := NodeCreate(ctx, nil, NodeUpsertRequest{RawURI: rawURI})
		if err != nil {
			t.Fatalf("NodeCreate(%q) error = %v", rawURI, err)
		}
		nodeIDs = append(nodeIDs, node.ID)
	}
	if _, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          nodeIDs,
	}); err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	options, _, err := BuildSingBoxOptions(ctx, nil)
	if err != nil {
		t.Fatalf("BuildSingBoxOptions() error = %v", err)
	}
	wantTypes := map[string]bool{
		constant.TypeShadowsocks: false,
		constant.TypeHysteria:    false,
		constant.TypeHysteria2:   false,
		constant.TypeTUIC:        false,
		constant.TypeSSH:         false,
	}
	for _, outbound := range options.Outbounds {
		if _, ok := wantTypes[outbound.Type]; ok {
			wantTypes[outbound.Type] = true
		}
	}
	for outboundType, found := range wantTypes {
		if !found {
			t.Fatalf("outbound type %q not found in %+v", outboundType, options.Outbounds)
		}
	}
}

func TestNodeCreateRejectsInvalidChain(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1081)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "jump",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node) error = %v", err)
	}

	_, err = NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:         "bad chain",
		Protocol:     ProtocolChain,
		ChainNodeIDs: []string{node.ID},
	})
	if !errors.Is(err, ErrInvalidChain) {
		t.Fatalf("NodeCreate(chain) error = %v, want %v", err, ErrInvalidChain)
	}
}

func findTestOutbound(outbounds []option.Outbound, tag string) *option.Outbound {
	for i := range outbounds {
		if outbounds[i].Tag == tag {
			return &outbounds[i]
		}
	}
	return nil
}

func TestBuildSingBoxOptionsRejectsCyclicGroups(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	first, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "first"})
	if err != nil {
		t.Fatalf("GroupCreate(first) error = %v", err)
	}
	second, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "second", GroupIDs: []string{first.ID}})
	if err != nil {
		t.Fatalf("GroupCreate(second) error = %v", err)
	}
	if _, err := GroupUpdate(ctx, nil, first.ID, GroupUpsertRequest{Name: "first", GroupIDs: []string{second.ID}}); err != nil {
		t.Fatalf("GroupUpdate(first) error = %v", err)
	}
	_, err = MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{first.ID},
		ActiveGroupID:    &first.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	_, _, err = BuildSingBoxOptions(ctx, nil)
	if err == nil {
		t.Fatalf("BuildSingBoxOptions() error = nil, want cyclic group error")
	}
}

func TestRuntimeReloadIsolatesFailedMapping(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	occupied := occupiedTCPPort(t)
	ctx := context.Background()
	failedMapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       occupied,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate(failed) error = %v", err)
	}
	runningMapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate(running) error = %v", err)
	}

	status, err := RuntimeReload(ctx)
	if err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	if !status.Running || status.State != "degraded" {
		t.Fatalf("Runtime status = %+v, want degraded and running", status)
	}
	if len(status.Inbounds) != 1 || status.Inbounds[0].MappingID != runningMapping.ID {
		t.Fatalf("Runtime inbounds = %+v, want only mapping %q", status.Inbounds, runningMapping.ID)
	}
	if len(status.Failures) != 1 || status.Failures[0].MappingID != failedMapping.ID {
		t.Fatalf("Runtime failures = %+v, want only mapping %q", status.Failures, failedMapping.ID)
	}
}

func TestRuntimeReloadExcludesInvalidNodeAndStartsMapping(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	badNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		RawURI: "vless://48a25c54-8826-4657-330e-8db38ef76716@example.com:443?security=tls&flow=bad-flow#bad",
	})
	if err != nil {
		t.Fatalf("NodeCreate(bad) error = %v", err)
	}
	goodPort := uint16(65000)
	goodNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "good socks",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &goodPort,
	})
	if err != nil {
		t.Fatalf("NodeCreate(good) error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{badNode.ID, goodNode.ID},
		ActiveNodeID:     &badNode.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	status, err := RuntimeReload(ctx)
	if err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	if !status.Running || status.State != "running" {
		t.Fatalf("Runtime status = %+v, want running", status)
	}
	if len(status.Inbounds) != 1 || status.Inbounds[0].MappingID != mapping.ID {
		t.Fatalf("Runtime inbounds = %+v, want mapping %q", status.Inbounds, mapping.ID)
	}
	if status.Inbounds[0].Outbound != mappingOutboundTag(mapping.ID) {
		t.Fatalf("Runtime outbound = %q, want mapping dynamic group tag", status.Inbounds[0].Outbound)
	}
	if len(status.Failures) != 0 {
		t.Fatalf("Runtime failures = %+v, want none", status.Failures)
	}
	if len(status.ExcludedNodes) != 1 || status.ExcludedNodes[0].NodeID != badNode.ID {
		t.Fatalf("Excluded nodes = %+v, want bad node %q", status.ExcludedNodes, badNode.ID)
	}
	health, err := getNodeHealth(ctx, nil, badNode.ID)
	if err != nil {
		t.Fatalf("getNodeHealth() error = %v", err)
	}
	if health == nil || !health.Blacklisted || health.Available {
		t.Fatalf("Bad node health = %+v, want blacklisted unavailable", health)
	}
}

func TestRuntimeReloadExcludesOnlyInvalidNodeToBlockRoute(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	badNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		RawURI: "vless://48a25c54-8826-4657-330e-8db38ef76716@example.com:443?security=tls&flow=bad-flow#bad",
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{badNode.ID},
		ActiveNodeID:     &badNode.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	status, err := RuntimeReload(ctx)
	if err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	if !status.Running || status.State != "running" {
		t.Fatalf("Runtime status = %+v, want running block-only mapping", status)
	}
	if len(status.Inbounds) != 1 || status.Inbounds[0].MappingID != mapping.ID {
		t.Fatalf("Runtime inbounds = %+v, want mapping %q", status.Inbounds, mapping.ID)
	}
	if status.Inbounds[0].Outbound != mappingOutboundTag(mapping.ID) {
		t.Fatalf("Runtime outbound = %q, want mapping dynamic group tag", status.Inbounds[0].Outbound)
	}
	if len(status.ExcludedNodes) != 1 || status.ExcludedNodes[0].NodeID != badNode.ID {
		t.Fatalf("Excluded nodes = %+v, want bad node %q", status.ExcludedNodes, badNode.ID)
	}
	if len(status.Failures) != 0 {
		t.Fatalf("Runtime failures = %+v, want none", status.Failures)
	}
}

func TestRuntimeReloadReportsAllMappingsFailed(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	occupied := occupiedTCPPort(t)
	ctx := context.Background()
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       occupied,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	status, err := RuntimeReload(ctx)
	if err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	if status.Running || status.State != "error" {
		t.Fatalf("Runtime status = %+v, want error and stopped", status)
	}
	if len(status.Inbounds) != 0 {
		t.Fatalf("Runtime inbounds = %+v, want none", status.Inbounds)
	}
	if len(status.Failures) != 1 || status.Failures[0].MappingID != mapping.ID {
		t.Fatalf("Runtime failures = %+v, want mapping %q", status.Failures, mapping.ID)
	}
}

func TestRuntimeSyncMappingDoesNotTouchUnrelatedFailures(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	occupied := occupiedTCPPort(t)
	ctx := context.Background()
	failedMapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       occupied,
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate(failed) error = %v", err)
	}
	runningMapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate(running) error = %v", err)
	}

	status, err := RuntimeSyncMapping(ctx, failedMapping.ID)
	if err != nil {
		t.Fatalf("RuntimeSyncMapping(failed) error = %v", err)
	}
	if status.Running || len(status.Failures) != 1 || status.Failures[0].MappingID != failedMapping.ID {
		t.Fatalf("status after failed sync = %+v, want only failed mapping", status)
	}

	status, err = RuntimeSyncMapping(ctx, runningMapping.ID)
	if err != nil {
		t.Fatalf("RuntimeSyncMapping(running) error = %v", err)
	}
	if !status.Running || status.State != "degraded" {
		t.Fatalf("status after running sync = %+v, want degraded running", status)
	}
	if len(status.Inbounds) != 1 || status.Inbounds[0].MappingID != runningMapping.ID {
		t.Fatalf("inbounds after running sync = %+v, want only running mapping", status.Inbounds)
	}
	if len(status.Failures) != 1 || status.Failures[0].MappingID != failedMapping.ID {
		t.Fatalf("failures after running sync = %+v, want preserved failed mapping", status.Failures)
	}
}

func TestRuntimeSyncMappingUpdatesDynamicGroupWithoutReplacingInstance(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	portA := uint16(65001)
	nodeA, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node-a",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &portA,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node-a) error = %v", err)
	}
	portB := uint16(65002)
	nodeB, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node-b",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &portB,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node-b) error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		NodeIDs:          []string{nodeA.ID},
		ActiveNodeID:     &nodeA.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	before := runtimeInstanceForMapping(mapping.ID)
	if before == nil {
		t.Fatalf("runtime instance was not created")
	}

	if _, err := MappingUpdate(ctx, nil, mapping.ID, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Strategy:         mapping.Strategy,
		NodeIDs:          []string{nodeA.ID, nodeB.ID},
		ActiveNodeID:     &nodeB.ID,
	}); err != nil {
		t.Fatalf("MappingUpdate() error = %v", err)
	}
	status, err := RuntimeSyncMapping(ctx, mapping.ID)
	if err != nil {
		t.Fatalf("RuntimeSyncMapping() error = %v", err)
	}
	after := runtimeInstanceForMapping(mapping.ID)
	if before != after {
		t.Fatalf("runtime instance was replaced during node-only update")
	}
	if len(status.Inbounds) != 1 || status.Inbounds[0].Outbound != mappingOutboundTag(mapping.ID) {
		t.Fatalf("status inbounds = %+v, want stable mapping dynamic group", status.Inbounds)
	}
	snapshot := after.core.Snapshot()
	var selected string
	var members []string
	for _, group := range snapshot.Groups {
		if group.Tag != mappingOutboundTag(mapping.ID) {
			continue
		}
		selected = group.Selected
		for _, node := range group.Nodes {
			members = append(members, node.ID)
		}
	}
	if selected != nodeB.ID {
		t.Fatalf("dynamic group selected = %q, want %q", selected, nodeB.ID)
	}
	if !containsString(members, nodeA.ID) || !containsString(members, nodeB.ID) {
		t.Fatalf("dynamic group members = %v, want node-a and node-b", members)
	}
}

func TestRuntimeSyncMappingExcludesInvalidGroupNodeAndKeepsInstance(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	goodPort := uint16(65006)
	goodNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "good",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &goodPort,
	})
	if err != nil {
		t.Fatalf("NodeCreate(good) error = %v", err)
	}
	badNode, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		RawURI: "vless://48a25c54-8826-4657-330e-8db38ef76716@example.com:443?security=tls&flow=bad-flow#bad",
	})
	if err != nil {
		t.Fatalf("NodeCreate(bad) error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:     "mixed group",
		Strategy: GroupStrategySelector,
		NodeIDs:  []string{badNode.ID, goodNode.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	before := runtimeInstanceForMapping(mapping.ID)
	if before == nil {
		t.Fatalf("runtime instance was not created")
	}

	if _, err := MappingUpdate(ctx, nil, mapping.ID, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Strategy:         mapping.Strategy,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	}); err != nil {
		t.Fatalf("MappingUpdate(add group) error = %v", err)
	}
	status, err := RuntimeSyncMapping(ctx, mapping.ID)
	if err != nil {
		t.Fatalf("RuntimeSyncMapping() error = %v", err)
	}
	after := runtimeInstanceForMapping(mapping.ID)
	if after != before {
		t.Fatalf("runtime instance was replaced while adding group")
	}
	if !status.Running || len(status.Failures) != 0 {
		t.Fatalf("Runtime status = %+v, want running without failures", status)
	}
	if len(status.ExcludedNodes) != 1 || status.ExcludedNodes[0].NodeID != badNode.ID {
		t.Fatalf("Excluded nodes = %+v, want bad node %q", status.ExcludedNodes, badNode.ID)
	}

	snapshot := after.core.Snapshot()
	var childGroupMembers []string
	for _, groupState := range snapshot.Groups {
		if groupState.Tag != proxyGroupOutboundTag(group.ID) {
			continue
		}
		for _, nodeState := range groupState.Nodes {
			childGroupMembers = append(childGroupMembers, nodeState.ID)
		}
	}
	if containsString(childGroupMembers, badNode.ID) {
		t.Fatalf("child dynamic group members = %v, want bad node excluded", childGroupMembers)
	}
	if !containsString(childGroupMembers, goodNode.ID) {
		t.Fatalf("child dynamic group members = %v, want good node %q", childGroupMembers, goodNode.ID)
	}
}

func TestRuntimeMappingCanRouteToExistingGroup(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	port := uint16(65003)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "group-node",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:     "existing group",
		Strategy: GroupStrategySelector,
		NodeIDs:  []string{node.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	status, err := RuntimeReload(ctx)
	if err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	if !status.Running {
		t.Fatalf("Runtime status = %+v, want running", status)
	}
	instance := runtimeInstanceForMapping(mapping.ID)
	if instance == nil {
		t.Fatalf("runtime instance was not created")
	}
	snapshot := instance.core.Snapshot()
	var mappingGroupMembers []string
	var childGroupMembers []string
	for _, groupState := range snapshot.Groups {
		switch groupState.Tag {
		case mappingOutboundTag(mapping.ID):
			for _, nodeState := range groupState.Nodes {
				mappingGroupMembers = append(mappingGroupMembers, nodeState.ID)
			}
		case proxyGroupOutboundTag(group.ID):
			for _, nodeState := range groupState.Nodes {
				childGroupMembers = append(childGroupMembers, nodeState.ID)
			}
		}
	}
	if !containsString(mappingGroupMembers, group.ID) {
		t.Fatalf("mapping dynamic group members = %v, want existing group %q", mappingGroupMembers, group.ID)
	}
	if !containsString(childGroupMembers, node.ID) {
		t.Fatalf("child dynamic group members = %v, want node %q", childGroupMembers, node.ID)
	}
}

func TestRuntimeMappingCanReaddExistingGroupWithoutReplacingInstance(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	port := uint16(65004)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "group-node",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:     "existing group",
		Strategy: GroupStrategySelector,
		NodeIDs:  []string{node.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	before := runtimeInstanceForMapping(mapping.ID)
	if before == nil {
		t.Fatalf("runtime instance was not created")
	}

	if _, err := MappingUpdate(ctx, nil, mapping.ID, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Strategy:         mapping.Strategy,
	}); err != nil {
		t.Fatalf("MappingUpdate(remove group) error = %v", err)
	}
	if _, err := RuntimeSyncMapping(ctx, mapping.ID); err != nil {
		t.Fatalf("RuntimeSyncMapping(remove group) error = %v", err)
	}
	if runtimeInstanceForMapping(mapping.ID) != before {
		t.Fatalf("runtime instance was replaced while removing group member")
	}

	if _, err := MappingUpdate(ctx, nil, mapping.ID, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    mapping.ListenAddress,
		ListenPort:       mapping.ListenPort,
		OutboundProtocol: mapping.OutboundProtocol,
		Strategy:         mapping.Strategy,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	}); err != nil {
		t.Fatalf("MappingUpdate(re-add group) error = %v", err)
	}
	if _, err := RuntimeSyncMapping(ctx, mapping.ID); err != nil {
		t.Fatalf("RuntimeSyncMapping(re-add group) error = %v", err)
	}
	after := runtimeInstanceForMapping(mapping.ID)
	if after != before {
		t.Fatalf("runtime instance was replaced while re-adding existing group")
	}

	snapshot := after.core.Snapshot()
	var mappingGroupMembers []string
	var childGroupMembers []string
	for _, groupState := range snapshot.Groups {
		switch groupState.Tag {
		case mappingOutboundTag(mapping.ID):
			for _, nodeState := range groupState.Nodes {
				mappingGroupMembers = append(mappingGroupMembers, nodeState.ID)
			}
		case proxyGroupOutboundTag(group.ID):
			for _, nodeState := range groupState.Nodes {
				childGroupMembers = append(childGroupMembers, nodeState.ID)
			}
		}
	}
	if !containsString(mappingGroupMembers, group.ID) {
		t.Fatalf("mapping dynamic group members after re-add = %v, want group %q", mappingGroupMembers, group.ID)
	}
	if !containsString(childGroupMembers, node.ID) {
		t.Fatalf("child dynamic group members after re-add = %v, want node %q", childGroupMembers, node.ID)
	}
}

func TestMappingTestResultIncludesSelectedNodeInfo(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "selected",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	group, err := GroupCreate(ctx, nil, GroupUpsertRequest{
		Name:     "manual",
		Strategy: GroupStrategySelector,
		NodeIDs:  []string{node.ID},
	})
	if err != nil {
		t.Fatalf("GroupCreate() error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyManual,
		GroupIDs:         []string{group.ID},
		ActiveGroupID:    &group.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}

	result, err := MappingTest(ctx, mapping.ID, ProxyTestRequest{ProbeURL: "https://example.com/generate_204"})
	if err != nil {
		t.Fatalf("MappingTest() error = %v", err)
	}
	if result.NodeID != node.ID || result.NodeName != node.Name || result.NodeTag != nodeOutboundTag(node.ID) {
		t.Fatalf("selected node info = id %q name %q tag %q, want node %q",
			result.NodeID,
			result.NodeName,
			result.NodeTag,
			node.ID,
		)
	}
}

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(:0) failed: %v", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	return uint16(addr.Port)
}

func occupiedTCPPort(t *testing.T) uint16 {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(:0) failed: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	return uint16(addr.Port)
}
