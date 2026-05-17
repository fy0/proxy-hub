package singboxcore

import (
	"errors"
	"fmt"

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
	parsed, err := proxyuri.ParseURI(rawURI)
	if err != nil {
		return nil, mapProxyURIError(err)
	}
	return parsed, nil
}

func OutboundFromURI(rawURI, tag string) (option.Outbound, error) {
	outbound, err := proxyuri.OutboundFromURI(rawURI, tag)
	if err != nil {
		return option.Outbound{}, mapProxyURIError(err)
	}
	return outbound, nil
}

func mapProxyURIError(err error) error {
	if err == nil {
		return nil
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
