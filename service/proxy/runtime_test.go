package proxy

import (
	"context"
	"net"
	"testing"

	"proxy-hub/model"

	"github.com/sagernet/sing-box/constant"
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
