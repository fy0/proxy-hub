package proxy

import (
	"context"
	"errors"
	"net"
	"testing"

	"proxy-hub/model"

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
	if status.Inbounds[0].Outbound != nodeOutboundTag(goodNode.ID) {
		t.Fatalf("Runtime outbound = %q, want good node tag", status.Inbounds[0].Outbound)
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
	if status.Inbounds[0].Outbound != constant.TypeBlock {
		t.Fatalf("Runtime outbound = %q, want block", status.Inbounds[0].Outbound)
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
