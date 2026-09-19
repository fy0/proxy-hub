package proxy

import (
	"github.com/sagernet/sing-box/option"

	"proxy-hub/service/proxyuri"
)

type parsedNodeURI = proxyuri.ParsedURI

func ParseNodeURI(rawURI string) (*NodeUpsertRequest, error) {
	parsed, err := parseNodeURI(rawURI)
	if err != nil {
		return nil, err
	}
	return parsedNodeToUpsertRequest(parsed), nil
}

func parseNodeURI(rawURI string) (*parsedNodeURI, error) {
	return proxyuri.ParseURI(rawURI)
}

func parsedNodeToUpsertRequest(parsed *parsedNodeURI) *NodeUpsertRequest {
	if parsed == nil {
		return nil
	}
	port := parsed.Port
	return &NodeUpsertRequest{
		Name:     parsed.Name,
		Protocol: parsed.Protocol,
		Server:   parsed.Server,
		Port:     &port,
		Username: parsed.Username,
		Password: parsed.Password,
		RawURI:   parsed.RawURI,
		Tags:     append([]string(nil), parsed.Tags...),
	}
}

func buildNodeOutboundFromURI(rawURI string, tag string) (option.Outbound, error) {
	return proxyuri.OutboundFromURIWithOptions(rawURI, tag, proxyuri.OutboundOptions{
		RequireUTLSSupport: true,
		UTLSAvailable:      withUTLS,
	})
}

func expandImportValue(value string) []string {
	return proxyuri.ExpandImportValue(value)
}

func clashProxyToURI(proxy map[string]any) string {
	return proxyuri.ClashProxyToURI(proxy)
}

func stringFromMap(values map[string]any, key string) string {
	return proxyuri.StringFromMap(values, key)
}

func boolFromMap(values map[string]any, key string) (bool, bool) {
	return proxyuri.BoolFromMap(values, key)
}
