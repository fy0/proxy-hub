package singboxcore

import (
	"github.com/sagernet/sing-box/option"

	"proxy-hub/service/proxyuri"
)

const (
	ProtocolHTTP        = proxyuri.ProtocolHTTP
	ProtocolSOCKS5      = proxyuri.ProtocolSOCKS5
	ProtocolShadowsocks = proxyuri.ProtocolShadowsocks
	ProtocolTrojan      = proxyuri.ProtocolTrojan
	ProtocolVMess       = proxyuri.ProtocolVMess
	ProtocolVLESS       = proxyuri.ProtocolVLESS
	ProtocolHysteria2   = proxyuri.ProtocolHysteria2
)

type ParsedURI = proxyuri.ParsedURI

func ParseURI(rawURI string) (*ParsedURI, error) {
	return proxyuri.ParseURI(rawURI)
}

func OutboundFromURI(rawURI, tag string) (option.Outbound, error) {
	return proxyuri.OutboundFromURI(rawURI, tag)
}
