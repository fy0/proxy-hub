package proxy

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"proxy-hub/model/tables"
)

func TestParseVLESSURI(t *testing.T) {
	raw := "vless://uuid@example.com:443?type=ws&security=tls&sni=edge.example.com&path=%2Fproxy#edge"

	node, err := ParseNodeURI(raw)
	if err != nil {
		t.Fatalf("ParseNodeURI() error = %v", err)
	}
	if node.Protocol != ProtocolVLESS {
		t.Fatalf("Protocol = %q, want %q", node.Protocol, ProtocolVLESS)
	}
	if node.Name != "edge" {
		t.Fatalf("Name = %q, want edge", node.Name)
	}
	if node.Server != "example.com" {
		t.Fatalf("Server = %q, want example.com", node.Server)
	}
	if node.Port == nil || *node.Port != 443 {
		t.Fatalf("Port = %v, want 443", node.Port)
	}
	if node.Username != "uuid" {
		t.Fatalf("Username = %q, want uuid", node.Username)
	}
}

func TestParseVMessURI(t *testing.T) {
	payload := `{"v":"2","ps":"vmess edge","add":"vmess.example.com","port":"443","id":"uuid","aid":"0","scy":"auto","net":"ws","type":"none","host":"cdn.example.com","path":"/ws","tls":"tls","sni":"vmess.example.com"}`
	raw := "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(payload))

	node, err := ParseNodeURI(raw)
	if err != nil {
		t.Fatalf("ParseNodeURI() error = %v", err)
	}
	if node.Protocol != ProtocolVMess {
		t.Fatalf("Protocol = %q, want %q", node.Protocol, ProtocolVMess)
	}
	if node.Name != "vmess edge" {
		t.Fatalf("Name = %q, want vmess edge", node.Name)
	}
	if node.Server != "vmess.example.com" {
		t.Fatalf("Server = %q, want vmess.example.com", node.Server)
	}
	if node.Port == nil || *node.Port != 443 {
		t.Fatalf("Port = %v, want 443", node.Port)
	}
}

func TestBuildNodeOutboundFromVLESSURI(t *testing.T) {
	raw := "vless://uuid@example.com:443?type=ws&security=tls&sni=edge.example.com&path=%2Fproxy&host=cdn.example.com#edge"

	outbound, err := buildNodeOutboundFromURI(raw, "node-test")
	if err != nil {
		t.Fatalf("buildNodeOutboundFromURI() error = %v", err)
	}
	if outbound.Type != constant.TypeVLESS {
		t.Fatalf("Type = %q, want %q", outbound.Type, constant.TypeVLESS)
	}

	options, ok := outbound.Options.(*option.VLESSOutboundOptions)
	if !ok {
		t.Fatalf("Options type = %T, want *option.VLESSOutboundOptions", outbound.Options)
	}
	if options.TLS == nil || !options.TLS.Enabled || options.TLS.ServerName != "edge.example.com" {
		t.Fatalf("TLS = %+v, want enabled with server name edge.example.com", options.TLS)
	}
	if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeWebsocket {
		t.Fatalf("Transport = %+v, want websocket", options.Transport)
	}
	if options.Transport.WebsocketOptions.Path != "/proxy" {
		t.Fatalf("Websocket path = %q, want /proxy", options.Transport.WebsocketOptions.Path)
	}
}

func TestBuildNodeOutboundFromVLESSRealityURI(t *testing.T) {
	raw := "vless://48a25c54-8826-4657-330e-8db38ef76716@us-n1.qq.org:6515?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.learn.microsoft.com&fp=chrome&pbk=j0WAnZjnHwzpiPwpHaurvyfqe1yZdbNeRG0isinebQc&spx=%2F&type=tcp&headerType=none#%E7%BE%8E%E8%A5%BFSJ_CN2"

	node, err := ParseNodeURI(raw)
	if err != nil {
		t.Fatalf("ParseNodeURI() error = %v", err)
	}
	if node.Name != "美西SJ_CN2" {
		t.Fatalf("Name = %q, want decoded fragment", node.Name)
	}
	if node.Server != "us-n1.qq.org" {
		t.Fatalf("Server = %q, want us-n1.qq.org", node.Server)
	}
	if node.Port == nil || *node.Port != 6515 {
		t.Fatalf("Port = %v, want 6515", node.Port)
	}

	outbound, err := buildNodeOutboundFromURI(raw, "node-test")
	if !withUTLS {
		if err != ErrUTLSRequired {
			t.Fatalf("buildNodeOutboundFromURI() error = %v, want ErrUTLSRequired", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("buildNodeOutboundFromURI() error = %v", err)
	}
	options, ok := outbound.Options.(*option.VLESSOutboundOptions)
	if !ok {
		t.Fatalf("Options type = %T, want *option.VLESSOutboundOptions", outbound.Options)
	}
	if options.Flow != "xtls-rprx-vision" {
		t.Fatalf("Flow = %q, want xtls-rprx-vision", options.Flow)
	}
	if options.Transport != nil {
		t.Fatalf("Transport = %+v, want nil for tcp transport", options.Transport)
	}
	if options.TLS == nil || !options.TLS.Enabled {
		t.Fatalf("TLS = %+v, want enabled", options.TLS)
	}
	if options.TLS.ServerName != "www.learn.microsoft.com" {
		t.Fatalf("TLS server name = %q, want www.learn.microsoft.com", options.TLS.ServerName)
	}
	if options.TLS.UTLS == nil || !options.TLS.UTLS.Enabled || options.TLS.UTLS.Fingerprint != "chrome" {
		t.Fatalf("UTLS = %+v, want chrome", options.TLS.UTLS)
	}
	if options.TLS.Reality == nil || !options.TLS.Reality.Enabled {
		t.Fatalf("Reality = %+v, want enabled", options.TLS.Reality)
	}
	if options.TLS.Reality.PublicKey != "j0WAnZjnHwzpiPwpHaurvyfqe1yZdbNeRG0isinebQc" {
		t.Fatalf("Reality public key = %q", options.TLS.Reality.PublicKey)
	}
	if options.TLS.Reality.ShortID != "" {
		t.Fatalf("Reality short ID = %q, want empty", options.TLS.Reality.ShortID)
	}
}

func TestBuildNodeOutboundFromVLESSH2URI(t *testing.T) {
	raw := "vless://uuid@example.com:443?type=h2&security=tls&sni=edge.example.com&host=cdn.example.com&path=%2Fh2#edge"

	node, err := ParseNodeURI(raw)
	if err != nil {
		t.Fatalf("ParseNodeURI() error = %v", err)
	}
	if !containsString(node.Tags, "h2") {
		t.Fatalf("Tags = %v, want h2 tag", node.Tags)
	}

	outbound, err := buildNodeOutboundFromURI(raw, "node-test")
	if err != nil {
		t.Fatalf("buildNodeOutboundFromURI() error = %v", err)
	}
	options, ok := outbound.Options.(*option.VLESSOutboundOptions)
	if !ok {
		t.Fatalf("Options type = %T, want *option.VLESSOutboundOptions", outbound.Options)
	}
	if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeHTTP {
		t.Fatalf("Transport = %+v, want http transport for h2 URI", options.Transport)
	}
	if options.Transport.HTTPOptions.Path != "/h2" {
		t.Fatalf("HTTP path = %q, want /h2", options.Transport.HTTPOptions.Path)
	}
	if len(options.Transport.HTTPOptions.Host) != 1 || options.Transport.HTTPOptions.Host[0] != "cdn.example.com" {
		t.Fatalf("HTTP host = %v, want cdn.example.com", options.Transport.HTTPOptions.Host)
	}
}

func TestBuildNodeOutboundFromVMessURL(t *testing.T) {
	raw := "vmess://uuid@example.com:443?security=auto&tls=tls&type=grpc&serviceName=edge-grpc#vmess-url"

	node, err := ParseNodeURI(raw)
	if err != nil {
		t.Fatalf("ParseNodeURI() error = %v", err)
	}
	if node.Protocol != ProtocolVMess {
		t.Fatalf("Protocol = %q, want %q", node.Protocol, ProtocolVMess)
	}
	if !containsString(node.Tags, "grpc") || !containsString(node.Tags, "tls") {
		t.Fatalf("Tags = %v, want grpc and tls", node.Tags)
	}

	outbound, err := buildNodeOutboundFromURI(raw, "node-test")
	if err != nil {
		t.Fatalf("buildNodeOutboundFromURI() error = %v", err)
	}
	options, ok := outbound.Options.(*option.VMessOutboundOptions)
	if !ok {
		t.Fatalf("Options type = %T, want *option.VMessOutboundOptions", outbound.Options)
	}
	if options.Security != "auto" {
		t.Fatalf("Security = %q, want auto", options.Security)
	}
	if options.TLS == nil || !options.TLS.Enabled {
		t.Fatalf("TLS = %+v, want enabled", options.TLS)
	}
	if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeGRPC {
		t.Fatalf("Transport = %+v, want grpc", options.Transport)
	}
	if options.Transport.GRPCOptions.ServiceName != "edge-grpc" {
		t.Fatalf("ServiceName = %q, want edge-grpc", options.Transport.GRPCOptions.ServiceName)
	}
}

func TestBuildNodeOutboundFromTrojanURI(t *testing.T) {
	raw := "trojan://secret@example.com:443?type=ws&sni=edge.example.com&path=%2Ftrojan#trojan"

	node, err := ParseNodeURI(raw)
	if err != nil {
		t.Fatalf("ParseNodeURI() error = %v", err)
	}
	if node.Username != "" || node.Password != "secret" {
		t.Fatalf("Username/Password = %q/%q, want empty/secret", node.Username, node.Password)
	}

	outbound, err := buildNodeOutboundFromURI(raw, "node-test")
	if err != nil {
		t.Fatalf("buildNodeOutboundFromURI() error = %v", err)
	}
	options, ok := outbound.Options.(*option.TrojanOutboundOptions)
	if !ok {
		t.Fatalf("Options type = %T, want *option.TrojanOutboundOptions", outbound.Options)
	}
	if options.Password != "secret" {
		t.Fatalf("Password = %q, want secret", options.Password)
	}
	if options.TLS == nil || !options.TLS.Enabled || options.TLS.ServerName != "edge.example.com" {
		t.Fatalf("TLS = %+v, want enabled with edge.example.com", options.TLS)
	}
	if options.Transport == nil || options.Transport.Type != constant.V2RayTransportTypeWebsocket {
		t.Fatalf("Transport = %+v, want websocket", options.Transport)
	}
}

func TestBuildNodeOutboundRejectsUnsupportedTransport(t *testing.T) {
	raw := "vless://uuid@example.com:443?type=xhttp#bad"

	_, err := buildNodeOutboundFromURI(raw, "node-test")
	if !errors.Is(err, ErrUnsupportedURI) {
		t.Fatalf("buildNodeOutboundFromURI() error = %v, want ErrUnsupportedURI", err)
	}
}

func TestBuildNodeOutboundRawURIDoesNotFallback(t *testing.T) {
	port := uint16(1080)
	node := &tables.ProxyNodeTable{
		Name:     "bad raw",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
		RawURI:   "vless://uuid@example.com:443?type=xhttp#bad",
	}

	_, err := buildNodeOutbound(node, "node-test")
	if !errors.Is(err, ErrUnsupportedURI) {
		t.Fatalf("buildNodeOutbound() error = %v, want ErrUnsupportedURI", err)
	}
}
