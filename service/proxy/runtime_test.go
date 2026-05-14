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
