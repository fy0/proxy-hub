package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/constant"

	"proxy-hub/model/tables"
)

func TestMappingProbeProxyURLAddressFamily(t *testing.T) {
	for _, tt := range []struct{ address, host string }{
		{"0.0.0.0", "127.0.0.1:1080"},
		{"127.0.0.1", "127.0.0.1:1080"},
		{"localhost", "127.0.0.1:1080"},
		{"::", "[::1]:1080"},
		{"[::]", "[::1]:1080"},
		{"::1", "[::1]:1080"},
		{"2001:db8::1", "[2001:db8::1]:1080"},
	} {
		t.Run(tt.address, func(t *testing.T) {
			mapping := &tables.PortMappingTable{ListenAddress: tt.address, ListenPort: 1080, OutboundProtocol: OutboundProtocolSOCKS}
			proxyURL, err := mappingProbeProxyURL(mapping)
			if err != nil || proxyURL.Host != tt.host || proxyURL.Scheme != "socks5" {
				t.Fatalf("proxy URL = %v, error = %v, want socks5://%s", proxyURL, err, tt.host)
			}
		})
	}
}

func TestSOCKSMappingListenAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "localhost", "::1", "[::1]"} {
		t.Run(address, func(t *testing.T) {
			initProxyInMemoryDB(t)
			t.Cleanup(func() { _ = RuntimeStop() })
			bindHost := "127.0.0.1"
			if address == "::1" || address == "[::1]" {
				bindHost = "::1"
			}
			listener, err := net.Listen("tcp", net.JoinHostPort(bindHost, "0"))
			if err != nil {
				t.Skipf("address unavailable on this host: %v", err)
			}
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			_ = listener.Close()
			mapping, err := MappingCreate(context.Background(), nil, MappingUpsertRequest{
				Enabled: true, ListenAddress: address, ListenPort: port,
				OutboundProtocol: OutboundProtocolSOCKS, Strategy: StrategyManual,
			})
			if err != nil {
				t.Fatalf("MappingCreate(%s) error = %v", address, err)
			}
			status, err := RuntimeSyncMapping(context.Background(), mapping.ID)
			if err != nil || !runtimeHasInboundForMapping(status, mapping.ID) || len(status.Failures) != 0 {
				t.Fatalf("start %s: status=%+v error=%v", address, status, err)
			}
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(bindHost, fmt.Sprint(port)), time.Second)
			if err != nil {
				t.Fatalf("connect to %s: %v", address, err)
			}
			_ = conn.Close()
		})
	}
}

func TestConcurrentRuntimeSyncDoesNotLeakListener(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() { _ = RuntimeStop() })
	mapping, err := MappingCreate(context.Background(), nil, MappingUpsertRequest{
		Enabled: true, ListenAddress: "127.0.0.1", ListenPort: freeTCPPort(t),
		OutboundProtocol: OutboundProtocolSOCKS, Strategy: StrategyManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := RuntimeSyncMapping(context.Background(), mapping.ID); err != nil {
				t.Errorf("RuntimeSyncMapping() error = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	status := RuntimeStatusGet()
	if !runtimeHasInboundForMapping(status, mapping.ID) || len(status.Failures) != 0 {
		t.Errorf("status after concurrent sync = %+v", status)
	}
	if err := RuntimeStop(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", mappingRuntimeListen(mapping))
	if err != nil {
		t.Fatalf("listener leaked after RuntimeStop: %v", err)
	}
	_ = listener.Close()
}

func TestRuntimeOutlivesCreationContext(t *testing.T) {
	initProxyInMemoryDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	mapping := &tables.PortMappingTable{
		ListenAddress: "127.0.0.1", ListenPort: freeTCPPort(t),
		OutboundProtocol: OutboundProtocolSOCKS, Strategy: StrategyManual,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan, err := buildDynamicRuntimePlanForMapping(ctx, nil, mapping, nil)
	if err != nil {
		t.Fatal(err)
	}
	group := dynamicGroupPlanByTag(plan.groups, mappingOutboundTag(mapping.ID))
	group.members = []dynamicMemberPlan{builtinMember(constant.TypeDirect)}
	group.selected = constant.TypeDirect
	instance, _, failure, nodeFailure := newRuntimeInstanceFromPlan(ctx, plan)
	if failure != nil || nodeFailure != nil {
		t.Fatalf("create runtime: failure=%+v nodeFailure=%+v", failure, nodeFailure)
	}
	defer instance.core.Close()
	cancel()
	proxyURL, err := mappingProbeProxyURL(mapping)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("runtime stopped with creation context: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}
}

func TestRuntimeSyncAppliesUpdatedNodeConfiguration(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() { _ = RuntimeStop() })
	ctx := context.Background()
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name: "upstream", Protocol: ProtocolSOCKS5, Server: "127.0.0.1", Port: uint16Ptr(65021),
	})
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled: true, ListenAddress: "127.0.0.1", ListenPort: freeTCPPort(t),
		OutboundProtocol: OutboundProtocolSOCKS, Strategy: StrategyManual,
		NodeIDs: []string{node.ID}, ActiveNodeID: &node.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeSyncMapping(ctx, mapping.ID); err != nil {
		t.Fatal(err)
	}
	before := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
	oldOutbound, ok := before.core.Box().Outbound().Outbound(nodeOutboundTag(node.ID))
	if !ok {
		t.Fatal("missing original outbound")
	}
	if _, err := NodeUpdate(ctx, nil, node.ID, NodeUpsertRequest{
		Name: node.Name, Protocol: ProtocolSOCKS5, Server: "127.0.0.1", Port: uint16Ptr(65022),
	}); err != nil {
		t.Fatal(err)
	}
	status, err := RuntimeSyncMapping(ctx, mapping.ID)
	if err != nil || len(status.Failures) != 0 {
		t.Fatalf("sync: status=%+v error=%v", status, err)
	}
	after := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
	newOutbound, ok := after.core.Box().Outbound().Outbound(nodeOutboundTag(node.ID))
	if !ok || newOutbound == oldOutbound {
		t.Fatal("runtime still uses the outbound created with the old node configuration")
	}
}
