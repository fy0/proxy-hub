package proxy

import (
	"context"
	"crypto/rand"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxy-hub/model"
)

// Opt in because these listeners bind to real network interfaces.
func TestSOCKSMappingLocalInterfaces(t *testing.T) {
	addresses := strings.TrimSpace(os.Getenv("PROXYHUB_TEST_LISTEN_ADDRESSES"))
	if addresses == "" {
		t.Skip("set PROXYHUB_TEST_LISTEN_ADDRESSES to comma-separated local IP addresses")
	}
	const responseBody = "proxyhub-local-interface-ok"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, responseBody)
	}))
	defer target.Close()

	for _, address := range strings.Split(addresses, ",") {
		address = strings.TrimSpace(address)
		t.Run(address, func(t *testing.T) {
			initProxyInMemoryDB(t)
			t.Cleanup(func() { _ = RuntimeStop() })
			ctx := context.Background()
			listener, err := net.Listen("tcp", net.JoinHostPort(address, "0"))
			if err != nil {
				t.Fatalf("OS bind to %s: %v", address, err)
			}
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			_ = listener.Close()

			group, err := GroupCreate(ctx, nil, GroupUpsertRequest{Name: "local-interface-test", Strategy: GroupStrategySelector})
			if err != nil {
				t.Fatal(err)
			}
			if err := model.GetTx(nil).Model(group).Update("builtin_tags_json", encodeStringSlice([]string{constantDirect})).Error; err != nil {
				t.Fatal(err)
			}
			mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
				Enabled: true, ListenAddress: address, ListenPort: port,
				OutboundProtocol: OutboundProtocolSOCKS, Strategy: StrategyManual,
				GroupIDs: []string{group.ID}, ActiveGroupID: &group.ID,
				Username: "interface-test", Password: rand.Text(),
			})
			if err != nil {
				t.Fatalf("MappingCreate(%s): %v", address, err)
			}
			status, err := RuntimeSyncMapping(ctx, mapping.ID)
			if err != nil || len(status.Failures) != 0 || !runtimeHasInboundForMapping(status, mapping.ID) {
				t.Fatalf("start SOCKS on %s: status=%+v error=%v", address, status, err)
			}
			endpoint := net.JoinHostPort(address, strconv.Itoa(int(port)))
			conn, err := net.DialTimeout("tcp", endpoint, 3*time.Second)
			if err != nil {
				t.Fatalf("connect SOCKS on %s: %v", endpoint, err)
			}
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, err = conn.Write([]byte{5, 1, 2})
			var reply [2]byte
			if err == nil {
				_, err = io.ReadFull(conn, reply[:])
			}
			_ = conn.Close()
			if err != nil || reply != [2]byte{5, 2} {
				t.Fatalf("SOCKS greeting on %s: reply=%v error=%v", endpoint, reply, err)
			}
			proxyURL, err := mappingProbeProxyURL(mapping)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			response, err := client.Get(target.URL)
			if err != nil {
				t.Fatalf("authenticated SOCKS forwarding on %s: %v", endpoint, err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK || string(body) != responseBody {
				t.Fatalf("forwarded response on %s: status=%d body=%q error=%v", endpoint, response.StatusCode, body, err)
			}
			transport.CloseIdleConnections()
			if _, err := RuntimeRemoveMapping(mapping.ID); err != nil {
				t.Fatal(err)
			}
			listener, err = net.Listen("tcp", endpoint)
			if err != nil {
				t.Fatalf("listener not released on %s: %v", endpoint, err)
			}
			_ = listener.Close()
			t.Logf("%s: create, bind, SOCKS5 authentication, HTTP forwarding, and listener cleanup passed", address)
		})
	}
}
