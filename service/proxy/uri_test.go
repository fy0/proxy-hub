package proxy

import (
	"encoding/base64"
	"testing"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
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
