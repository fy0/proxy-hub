package proxy

import (
	"context"
	"net"
	"testing"

	"proxy-hub/model"
	proxyService "proxy-hub/service/proxy"

	"gorm.io/gorm/logger"
)

func TestReloadRuntimeAfterMutationIgnoresBindFailure(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(func() {
		_ = proxyService.RuntimeStop()
	})

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

	_, err = proxyService.MappingCreate(context.Background(), nil, proxyService.MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       uint16(addr.Port),
		OutboundProtocol: proxyService.OutboundProtocolMixed,
		Strategy:         proxyService.StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	if err := reloadRuntimeAfterMutation(); err != nil {
		t.Fatalf("reloadRuntimeAfterMutation() error = %v, want nil", err)
	}
}
