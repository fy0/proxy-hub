package proxy

import (
	"errors"
	"fmt"
	"strings"

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
	parsed, err := proxyuri.ParseURI(rawURI)
	if err != nil {
		return nil, mapProxyURIError(err)
	}
	return parsed, nil
}

func parseVMessURI(rawURI string) (*NodeUpsertRequest, error) {
	parsed, err := parseNodeURI(rawURI)
	if err != nil {
		return nil, err
	}
	if parsed.Protocol != ProtocolVMess {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProtocol, parsed.Protocol)
	}
	return parsedNodeToUpsertRequest(parsed), nil
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
	outbound, err := proxyuri.OutboundFromURIWithOptions(rawURI, tag, proxyuri.OutboundOptions{
		RequireUTLSSupport: true,
		UTLSAvailable:      withUTLS,
	})
	if err != nil {
		return option.Outbound{}, mapProxyURIError(err)
	}
	return outbound, nil
}

func expandImportValue(value string) []string {
	return proxyuri.ExpandImportValue(value)
}

func clashProxyURIs(raw string) []string {
	return proxyuri.ClashProxyURIs(raw)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func mapProxyURIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, proxyuri.ErrUTLSRequired) {
		return ErrUTLSRequired
	}
	if errors.Is(err, proxyuri.ErrUnsupportedProtocol) {
		return replaceWrappedError(err, proxyuri.ErrUnsupportedProtocol, ErrUnsupportedProtocol)
	}
	if errors.Is(err, proxyuri.ErrInvalidPort) {
		return replaceWrappedError(err, proxyuri.ErrInvalidPort, ErrInvalidPort)
	}
	if errors.Is(err, proxyuri.ErrUnsupportedURI) {
		return replaceWrappedError(err, proxyuri.ErrUnsupportedURI, ErrUnsupportedURI)
	}
	return err
}

func replaceWrappedError(err error, from error, to error) error {
	if err == from {
		return to
	}
	message := err.Error()
	fromMessage := from.Error()
	if message == fromMessage {
		return to
	}
	if len(message) > len(fromMessage) && message[:len(fromMessage)] == fromMessage {
		return fmt.Errorf("%w%s", to, message[len(fromMessage):])
	}
	return fmt.Errorf("%w: %v", to, err)
}
