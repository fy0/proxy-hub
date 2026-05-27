package singboxcore

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

func TestEmbeddedDynamicGroupDoesNotDropExistingStream(t *testing.T) {
	uriA := os.Getenv("TEST_PROXY_URI_A")
	uriB := os.Getenv("TEST_PROXY_URI_B")
	if uriA == "" || uriB == "" {
		t.Skip("set TEST_PROXY_URI_A and TEST_PROXY_URI_B to run sing-box integration test")
	}

	nodeA, err := OutboundFromURI(uriA, "node-a")
	if err != nil {
		t.Fatalf("OutboundFromURI(node-a) error = %v", err)
	}
	nodeB, err := OutboundFromURI(uriB, "node-b")
	if err != nil {
		t.Fatalf("OutboundFromURI(node-b) error = %v", err)
	}

	proxyPort := freeTCPPort(t)
	listen := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	core, err := NewCore(Config{
		Context: context.Background(),
		Options: option.Options{
			Log: &option.LogOptions{
				Disabled: true,
				Level:    "error",
			},
			Inbounds: []option.Inbound{
				{
					Type: C.TypeMixed,
					Tag:  "mixed-in",
					Options: &option.HTTPMixedInboundOptions{
						ListenOptions: option.ListenOptions{
							Listen:     &listen,
							ListenPort: proxyPort,
						},
					},
				},
			},
			Outbounds: append(BaseOutbounds(), nodeA),
			Route: &option.RouteOptions{
				Final: C.TypeDirect,
				Rules: []option.Rule{
					{
						Type: C.RuleTypeDefault,
						DefaultOptions: option.DefaultRule{
							RawDefaultRule: option.RawDefaultRule{
								Inbound: badoption.Listable[string]{"mixed-in"},
							},
							RuleAction: option.RuleAction{
								Action: C.RuleActionTypeRoute,
								RouteOptions: option.RouteActionOptions{
									Outbound: "group-auto",
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewCore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = core.Close()
	})
	if _, err := core.UpsertGroup("group-auto", Policy{Strategy: BalanceManual, RemoveTTL: time.Second}); err != nil {
		t.Fatalf("UpsertGroup() error = %v", err)
	}
	if err := core.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := core.AddNodeOutbound("group-auto", NodeConfig{ID: "node-a", Tag: "node-a", Outbound: nodeA}); err != nil {
		t.Fatalf("AddNodeOutbound(node-a) error = %v", err)
	}

	client := proxiedClient(t, proxyPort, 20*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	streamURL := getenvDefault("TEST_STREAM_URL", "https://speed.cloudflare.com/__down?bytes=10485760")
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		t.Fatalf("NewRequest(stream) error = %v", err)
	}
	streamResp, err := client.Do(streamReq)
	if err != nil {
		t.Fatalf("open stream through node-a: %v", err)
	}
	defer streamResp.Body.Close()
	buf := make([]byte, 256)
	if _, err := streamResp.Body.Read(buf); err != nil {
		t.Fatalf("initial stream read: %v", err)
	}

	if err := core.AddNodeOutbound("group-auto", NodeConfig{ID: "node-b", Tag: "node-b", Outbound: nodeB}); err != nil {
		t.Fatalf("AddNodeOutbound(node-b) error = %v", err)
	}
	if err := core.SelectNode("group-auto", "node-b"); err != nil {
		t.Fatalf("SelectNode(node-b) error = %v", err)
	}
	if err := core.DisableNode("group-auto", "node-a"); err != nil {
		t.Fatalf("DisableNode(node-a) error = %v", err)
	}

	ipURL := getenvDefault("TEST_IP_URL", "https://api.ipify.org")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipURL, nil)
	if err != nil {
		t.Fatalf("NewRequest(ip) error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("new request after switching to node-b: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	_ = resp.Body.Close()

	if _, err := streamResp.Body.Read(buf); err != nil {
		t.Fatalf("existing stream should remain readable after disable: %v", err)
	}
	if err := core.RemoveNode("group-auto", "node-a"); err != nil {
		t.Fatalf("RemoveNode(node-a) error = %v", err)
	}
	state := core.Snapshot()
	nodeAState := findSnapshotNode(state, "group-auto", "node-a")
	if nodeAState == nil || !nodeAState.Tombstoned {
		t.Fatalf("node-a snapshot = %+v, want tombstoned before stream close", nodeAState)
	}
	_ = streamResp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = core.GC()
		if findSnapshotNode(core.Snapshot(), "group-auto", "node-a") == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("node-a was not GC'd after stream close")
}

func TestStartReturnsPortInUseError(t *testing.T) {
	port := occupiedTCPPort(t)
	listen := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	core, err := NewCore(Config{
		Context: context.Background(),
		Options: option.Options{
			Log: &option.LogOptions{Disabled: true},
			Inbounds: []option.Inbound{
				{
					Type: C.TypeMixed,
					Tag:  "mixed-in",
					Options: &option.HTTPMixedInboundOptions{
						ListenOptions: option.ListenOptions{
							Listen:     &listen,
							ListenPort: port,
						},
					},
				},
			},
			Outbounds: BaseOutbounds(),
		},
	})
	if err != nil {
		t.Fatalf("NewCore() error = %v", err)
	}
	defer core.Close()
	err = core.Start()
	if err == nil {
		t.Fatalf("Start() error = nil, want port in use")
	}
	var portErr *PortInUseError
	if !errors.As(err, &portErr) {
		t.Fatalf("Start() error = %T %v, want PortInUseError", err, err)
	}
}

func TestBoxContextRegistersSelectorOutbound(t *testing.T) {
	core, err := NewCore(Config{
		Context: context.Background(),
		Options: option.Options{
			Log: &option.LogOptions{Disabled: true},
			Outbounds: append(BaseOutbounds(), option.Outbound{
				Type: C.TypeSelector,
				Tag:  "select-direct",
				Options: &option.SelectorOutboundOptions{
					Outbounds: []string{C.TypeDirect},
					Default:   C.TypeDirect,
				},
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewCore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = core.Close()
	})
	if err := core.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestRemoveGroupReleasesExclusiveOutbound(t *testing.T) {
	core, err := NewCore(Config{
		Context: context.Background(),
		Options: option.Options{
			Log:       &option.LogOptions{Disabled: true},
			Outbounds: BaseOutbounds(),
		},
	})
	if err != nil {
		t.Fatalf("NewCore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = core.Close()
	})
	if _, err := core.UpsertGroup("group-a", Policy{Strategy: BalanceManual, RemoveTTL: time.Second}); err != nil {
		t.Fatalf("UpsertGroup() error = %v", err)
	}
	if err := core.AddNodeOutbound("group-a", NodeConfig{ID: "node-a", Tag: "node-a", Outbound: option.Outbound{Type: C.TypeDirect, Tag: "node-a", Options: &option.DirectOutboundOptions{}}}); err != nil {
		t.Fatalf("AddNodeOutbound() error = %v", err)
	}
	if _, exists := core.Box().Outbound().Outbound("node-a"); !exists {
		t.Fatalf("node-a outbound missing before group removal")
	}
	if err := core.RemoveGroup("group-a"); err != nil {
		t.Fatalf("RemoveGroup() error = %v", err)
	}
	if _, exists := core.Box().Outbound().Outbound("node-a"); exists {
		t.Fatalf("node-a outbound still exists after group removal")
	}
	if _, exists := core.Box().Outbound().Outbound("group-a"); exists {
		t.Fatalf("group-a outbound still exists after group removal")
	}
}

func proxiedClient(t *testing.T, port uint16, timeout time.Duration) *http.Client {
	t.Helper()
	proxyURL, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(int(port)))
	if err != nil {
		t.Fatalf("proxy URL parse error: %v", err)
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
}

func findSnapshotNode(state CoreState, groupTag, nodeID string) *NodeSnapshot {
	for _, group := range state.Groups {
		if group.Tag != groupTag {
			continue
		}
		for _, node := range group.Nodes {
			if node.ID == nodeID {
				copyNode := node
				return &copyNode
			}
		}
	}
	return nil
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(:0) failed: %v", err)
	}
	defer listener.Close()
	addr := listener.Addr().(*net.TCPAddr)
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
	addr := listener.Addr().(*net.TCPAddr)
	return uint16(addr.Port)
}
