package proxyuri

import (
	"encoding/base64"
	"testing"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

func TestParseURICoversCommonProtocols(t *testing.T) {
	vmessPayload := `{"v":"2","ps":"vmess edge","add":"vmess.example.com","port":"443","id":"uuid","aid":"2","scy":"auto","net":"ws","host":"cdn.example.com","path":"/ws","tls":"tls","sni":"vmess.example.com"}`
	legacySSPayload := base64.RawStdEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:legacy@legacy.example.com:8388"))
	tests := []struct {
		name     string
		raw      string
		protocol string
		server   string
		port     uint16
		user     string
		password string
	}{
		{
			name:     "vless",
			raw:      "vless://uuid@example.com:443?type=ws&security=tls&sni=edge.example.com&path=%2Fproxy#edge",
			protocol: ProtocolVLESS,
			server:   "example.com",
			port:     443,
			user:     "uuid",
		},
		{
			name:     "vmess base64",
			raw:      "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(vmessPayload)),
			protocol: ProtocolVMess,
			server:   "vmess.example.com",
			port:     443,
			user:     "uuid",
		},
		{
			name:     "vmess url",
			raw:      "vmess://uuid@example.com:443?security=auto&tls=tls&type=grpc&serviceName=edge#vmess-url",
			protocol: ProtocolVMess,
			server:   "example.com",
			port:     443,
			user:     "uuid",
		},
		{
			name:     "trojan",
			raw:      "trojan://secret@example.com:443?type=ws&sni=edge.example.com&path=%2Ftrojan#trojan",
			protocol: ProtocolTrojan,
			server:   "example.com",
			port:     443,
			password: "secret",
		},
		{
			name:     "shadowsocks",
			raw:      "ss://aes-128-gcm:secret@ss.example.com:8388?network=udp#ss",
			protocol: ProtocolShadowsocks,
			server:   "ss.example.com",
			port:     8388,
			user:     "aes-128-gcm",
			password: "secret",
		},
		{
			name:     "legacy shadowsocks",
			raw:      "ss://" + legacySSPayload + "#legacy",
			protocol: ProtocolShadowsocks,
			server:   "legacy.example.com",
			port:     8388,
			user:     "2022-blake3-aes-128-gcm",
			password: "legacy",
		},
		{
			name:     "socks",
			raw:      "socks://user:pass@socks.example.com:1081#socks",
			protocol: ProtocolSOCKS5,
			server:   "socks.example.com",
			port:     1081,
			user:     "user",
			password: "pass",
		},
		{
			name:     "http",
			raw:      "http://user:pass@http.example.com:8080#http",
			protocol: ProtocolHTTP,
			server:   "http.example.com",
			port:     8080,
			user:     "user",
			password: "pass",
		},
		{
			name:     "https",
			raw:      "https://user:pass@secure.example.com#https",
			protocol: ProtocolHTTP,
			server:   "secure.example.com",
			port:     443,
			user:     "user",
			password: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseURI(tt.raw)
			if err != nil {
				t.Fatalf("ParseURI() error = %v", err)
			}
			if parsed.Protocol != tt.protocol || parsed.Server != tt.server || parsed.Port != tt.port || parsed.Username != tt.user || parsed.Password != tt.password {
				t.Fatalf("parsed = %+v, want protocol/server/port/user/password %q/%q/%d/%q/%q", parsed, tt.protocol, tt.server, tt.port, tt.user, tt.password)
			}
			if tt.name == "https" && parsed.Query.Get("security") != "tls" {
				t.Fatalf("https security = %q, want tls", parsed.Query.Get("security"))
			}
		})
	}
}

func TestOutboundFromURIKeyFields(t *testing.T) {
	t.Run("vless websocket tls", func(t *testing.T) {
		raw := "vless://uuid@example.com:443?type=ws&security=tls&sni=edge.example.com&path=%2Fproxy&host=cdn.example.com#edge"
		outbound, err := OutboundFromURI(raw, "node-vless")
		if err != nil {
			t.Fatalf("OutboundFromURI() error = %v", err)
		}
		if outbound.Type != constant.TypeVLESS || outbound.Tag != "node-vless" {
			t.Fatalf("outbound = %+v, want vless/node-vless", outbound)
		}
		options, ok := outbound.Options.(*option.VLESSOutboundOptions)
		if !ok {
			t.Fatalf("Options type = %T, want *option.VLESSOutboundOptions", outbound.Options)
		}
		if options.UUID != "uuid" || options.Server != "example.com" || options.ServerPort != 443 {
			t.Fatalf("options = %+v, want server uuid fields", options)
		}
		if options.TLS == nil || !options.TLS.Enabled || options.TLS.ServerName != "edge.example.com" {
			t.Fatalf("TLS = %+v, want enabled edge.example.com", options.TLS)
		}
		if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeWebsocket || options.Transport.WebsocketOptions.Path != "/proxy" {
			t.Fatalf("Transport = %+v, want websocket /proxy", options.Transport)
		}
	})

	t.Run("vmess grpc tls", func(t *testing.T) {
		raw := "vmess://uuid@example.com:443?security=auto&tls=tls&type=grpc&serviceName=edge-grpc#vmess-url"
		outbound, err := OutboundFromURI(raw, "node-vmess")
		if err != nil {
			t.Fatalf("OutboundFromURI() error = %v", err)
		}
		options, ok := outbound.Options.(*option.VMessOutboundOptions)
		if !ok {
			t.Fatalf("Options type = %T, want *option.VMessOutboundOptions", outbound.Options)
		}
		if outbound.Type != constant.TypeVMess || options.UUID != "uuid" || options.Security != "auto" {
			t.Fatalf("outbound/options = %+v/%+v, want vmess uuid auto", outbound, options)
		}
		if options.TLS == nil || !options.TLS.Enabled {
			t.Fatalf("TLS = %+v, want enabled", options.TLS)
		}
		if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeGRPC || options.Transport.GRPCOptions.ServiceName != "edge-grpc" {
			t.Fatalf("Transport = %+v, want grpc edge-grpc", options.Transport)
		}
	})

	t.Run("trojan websocket", func(t *testing.T) {
		raw := "trojan://secret@example.com:443?type=ws&sni=edge.example.com&path=%2Ftrojan#trojan"
		outbound, err := OutboundFromURI(raw, "node-trojan")
		if err != nil {
			t.Fatalf("OutboundFromURI() error = %v", err)
		}
		options, ok := outbound.Options.(*option.TrojanOutboundOptions)
		if !ok {
			t.Fatalf("Options type = %T, want *option.TrojanOutboundOptions", outbound.Options)
		}
		if outbound.Type != constant.TypeTrojan || options.Password != "secret" || options.TLS == nil || !options.TLS.Enabled {
			t.Fatalf("outbound/options = %+v/%+v, want trojan password tls", outbound, options)
		}
	})

	t.Run("shadowsocks udp", func(t *testing.T) {
		raw := "ss://aes-128-gcm:secret@ss.example.com:8388?network=udp#ss"
		outbound, err := OutboundFromURI(raw, "node-ss")
		if err != nil {
			t.Fatalf("OutboundFromURI() error = %v", err)
		}
		options, ok := outbound.Options.(*option.ShadowsocksOutboundOptions)
		if !ok {
			t.Fatalf("Options type = %T, want *option.ShadowsocksOutboundOptions", outbound.Options)
		}
		if outbound.Type != constant.TypeShadowsocks || options.Method != "aes-128-gcm" || options.Password != "secret" || options.Network != "udp" {
			t.Fatalf("outbound/options = %+v/%+v, want shadowsocks credentials udp", outbound, options)
		}
	})

	t.Run("socks auth", func(t *testing.T) {
		outbound, err := OutboundFromURI("socks5://user:pass@socks.example.com:1080#socks", "node-socks")
		if err != nil {
			t.Fatalf("OutboundFromURI() error = %v", err)
		}
		options, ok := outbound.Options.(*option.SOCKSOutboundOptions)
		if !ok {
			t.Fatalf("Options type = %T, want *option.SOCKSOutboundOptions", outbound.Options)
		}
		if outbound.Type != constant.TypeSOCKS || options.Username != "user" || options.Password != "pass" || options.Version != "5" {
			t.Fatalf("outbound/options = %+v/%+v, want socks auth", outbound, options)
		}
	})

	t.Run("http auth", func(t *testing.T) {
		outbound, err := OutboundFromURI("http://user:pass@http.example.com:8080#http", "node-http")
		if err != nil {
			t.Fatalf("OutboundFromURI() error = %v", err)
		}
		options, ok := outbound.Options.(*option.HTTPOutboundOptions)
		if !ok {
			t.Fatalf("Options type = %T, want *option.HTTPOutboundOptions", outbound.Options)
		}
		if outbound.Type != constant.TypeHTTP || options.Username != "user" || options.Password != "pass" || options.ServerPort != 8080 {
			t.Fatalf("outbound/options = %+v/%+v, want http auth", outbound, options)
		}
	})
}

func TestClashProxyURIs(t *testing.T) {
	raw := `proxies:
  - name: edge
    type: vless
    server: example.com
    port: 443
    uuid: uuid
    network: ws
    tls: true
    servername: edge.example.com
    ws-opts:
      path: /proxy
      headers:
        Host: cdn.example.com
`
	uris := ClashProxyURIs(raw)
	if len(uris) != 1 {
		t.Fatalf("ClashProxyURIs() len = %d, want 1: %v", len(uris), uris)
	}
	parsed, err := ParseURI(uris[0])
	if err != nil {
		t.Fatalf("ParseURI(clash uri) error = %v", err)
	}
	if parsed.Protocol != ProtocolVLESS || parsed.Name != "edge" || parsed.Query.Get("type") != "ws" || parsed.Query.Get("security") != "tls" || parsed.Query.Get("host") != "cdn.example.com" {
		t.Fatalf("parsed clash uri = %+v, query = %v", parsed, parsed.Query)
	}
}
