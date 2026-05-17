package proxyuri

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/byteformats"
	"github.com/sagernet/sing/common/json/badoption"
)

type OutboundOptions struct {
	RequireUTLSSupport bool
	UTLSAvailable      bool
}

func OutboundFromURI(rawURI string, tag string) (option.Outbound, error) {
	return OutboundFromURIWithOptions(rawURI, tag, OutboundOptions{})
}

func OutboundFromURIWithOptions(rawURI string, tag string, options OutboundOptions) (option.Outbound, error) {
	parsed, err := ParseURI(rawURI)
	if err != nil {
		return option.Outbound{}, err
	}
	serverOptions := option.ServerOptions{
		Server:     parsed.Server,
		ServerPort: parsed.Port,
	}

	switch parsed.Protocol {
	case ProtocolVLESS:
		if options.RequireUTLSSupport && requiresUTLS(parsed.Query) && !options.UTLSAvailable {
			return option.Outbound{}, ErrUTLSRequired
		}
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeVLESS,
			Tag:  tag,
			Options: &option.VLESSOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          parsed.Username,
				Flow:          normalizeVLESSFlow(queryFirst(parsed.Query, "flow")),
				PacketEncoding: stringPtrOrNil(
					queryFirst(parsed.Query, "packetEncoding", "packet_encoding", "packet-encoding"),
				),
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
				Transport:                   transport,
			},
		}, nil
	case ProtocolVMess:
		if options.RequireUTLSSupport && requiresUTLS(parsed.Query) && !options.UTLSAvailable {
			return option.Outbound{}, ErrUTLSRequired
		}
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeVMess,
			Tag:  tag,
			Options: &option.VMessOutboundOptions{
				ServerOptions:  serverOptions,
				UUID:           parsed.Username,
				Security:       firstNonEmpty(parsed.VMessSecurity, "auto"),
				AlterId:        parsed.VMessAlterID,
				PacketEncoding: parsed.VMessPacketEncoding,
				Transport:      transport,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
			},
		}, nil
	case ProtocolTrojan:
		if options.RequireUTLSSupport && requiresUTLS(parsed.Query) && !options.UTLSAvailable {
			return option.Outbound{}, ErrUTLSRequired
		}
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeTrojan,
			Tag:  tag,
			Options: &option.TrojanOutboundOptions{
				ServerOptions:               serverOptions,
				Password:                    parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
				Transport:                   transport,
			},
		}, nil
	case ProtocolSOCKS5:
		return option.Outbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SOCKSOutboundOptions{
				ServerOptions: serverOptions,
				Version:       "5",
				Username:      parsed.Username,
				Password:      parsed.Password,
			},
		}, nil
	case ProtocolHTTP:
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPOutboundOptions{
				ServerOptions:               serverOptions,
				Username:                    parsed.Username,
				Password:                    parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
			},
		}, nil
	case ProtocolShadowsocks:
		return buildShadowsocksOutbound(parsed, serverOptions, tag)
	case ProtocolHysteria:
		return buildHysteriaOutbound(parsed, serverOptions, tag)
	case ProtocolHysteria2:
		return buildHysteria2Outbound(parsed, serverOptions, tag)
	case ProtocolTUIC:
		return buildTUICOutbound(parsed, serverOptions, tag)
	case ProtocolSSH:
		return buildSSHOutbound(parsed, serverOptions, tag)
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func buildShadowsocksOutbound(parsed *ParsedURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	if strings.TrimSpace(parsed.Username) == "" || strings.TrimSpace(parsed.Password) == "" {
		return option.Outbound{}, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
	}
	options := &option.ShadowsocksOutboundOptions{
		ServerOptions: serverOptions,
		Method:        parsed.Username,
		Password:      parsed.Password,
		Plugin:        queryFirst(parsed.Query, "plugin"),
		PluginOptions: queryFirst(parsed.Query, "plugin_opts", "plugin-opts", "pluginOptions"),
		Network:       networkListFromQuery(parsed.Query),
	}
	return option.Outbound{Type: constant.TypeShadowsocks, Tag: tag, Options: options}, nil
}

func buildHysteriaOutbound(parsed *ParsedURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
	if err != nil {
		return option.Outbound{}, err
	}
	options := &option.HysteriaOutboundOptions{
		ServerOptions: serverOptions,
		ServerPorts:   listableStringFromQuery(parsed.Query, "server_ports", "server-ports", "ports"),
		HopInterval:   durationFromQuery(parsed.Query, "hop_interval", "hop-interval"),
		Obfs:          queryFirst(parsed.Query, "obfs"),
		AuthString:    firstNonEmpty(parsed.Password, queryFirst(parsed.Query, "auth_str", "auth-str", "password")),
		Network:       networkListFromQuery(parsed.Query),
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
	}
	if options.AuthString == "" {
		if auth := queryFirst(parsed.Query, "auth"); auth != "" {
			if decoded, err := decodeBase64Flexible(auth); err == nil {
				options.Auth = decoded
			} else {
				options.AuthString = auth
			}
		}
	}
	if up, err := networkBytesFromQuery(parsed.Query, "up"); err != nil {
		return option.Outbound{}, err
	} else {
		options.Up = up
	}
	if down, err := networkBytesFromQuery(parsed.Query, "down"); err != nil {
		return option.Outbound{}, err
	} else {
		options.Down = down
	}
	options.UpMbps = intFromQuery(parsed.Query, "up_mbps", "up-mbps", "upmbps")
	options.DownMbps = intFromQuery(parsed.Query, "down_mbps", "down-mbps", "downmbps")
	if options.Up == nil && options.UpMbps == 0 {
		return option.Outbound{}, fmt.Errorf("%w: missing hysteria upload bandwidth", ErrUnsupportedURI)
	}
	if options.Down == nil && options.DownMbps == 0 {
		return option.Outbound{}, fmt.Errorf("%w: missing hysteria download bandwidth", ErrUnsupportedURI)
	}
	options.ReceiveWindowConn = uint64FromQuery(parsed.Query, "recv_window_conn", "recv-window-conn")
	options.ReceiveWindow = uint64FromQuery(parsed.Query, "recv_window", "recv-window")
	options.DisableMTUDiscovery = queryBool(parsed.Query, "disable_mtu_discovery", "disable-mtu-discovery")
	return option.Outbound{Type: constant.TypeHysteria, Tag: tag, Options: options}, nil
}

func buildHysteria2Outbound(parsed *ParsedURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	if strings.TrimSpace(parsed.Password) == "" {
		return option.Outbound{}, fmt.Errorf("%w: missing hysteria2 password", ErrUnsupportedURI)
	}
	tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
	if err != nil {
		return option.Outbound{}, err
	}
	options := &option.Hysteria2OutboundOptions{
		ServerOptions: serverOptions,
		ServerPorts:   listableStringFromQuery(parsed.Query, "server_ports", "server-ports", "ports"),
		HopInterval:   durationFromQuery(parsed.Query, "hop_interval", "hop-interval"),
		UpMbps:        intFromQuery(parsed.Query, "up_mbps", "up-mbps", "upmbps"),
		DownMbps:      intFromQuery(parsed.Query, "down_mbps", "down-mbps", "downmbps"),
		Password:      parsed.Password,
		Network:       networkListFromQuery(parsed.Query),
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
		BrutalDebug: queryBool(parsed.Query, "brutal_debug", "brutal-debug"),
	}
	if obfsPassword := queryFirst(parsed.Query, "obfs-password", "obfs_password", "obfsPassword"); obfsPassword != "" {
		options.Obfs = &option.Hysteria2Obfs{
			Type:     firstNonEmpty(queryFirst(parsed.Query, "obfs", "obfs-type", "obfs_type"), "salamander"),
			Password: obfsPassword,
		}
	}
	return option.Outbound{Type: constant.TypeHysteria2, Tag: tag, Options: options}, nil
}

func buildTUICOutbound(parsed *ParsedURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	if strings.TrimSpace(parsed.Username) == "" {
		return option.Outbound{}, fmt.Errorf("%w: missing tuic uuid", ErrUnsupportedURI)
	}
	tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
	if err != nil {
		return option.Outbound{}, err
	}
	options := &option.TUICOutboundOptions{
		ServerOptions:     serverOptions,
		UUID:              parsed.Username,
		Password:          parsed.Password,
		CongestionControl: firstNonEmpty(queryFirst(parsed.Query, "congestion_control", "congestion-control"), "cubic"),
		UDPRelayMode:      queryFirst(parsed.Query, "udp_relay_mode", "udp-relay-mode"),
		UDPOverStream:     queryBool(parsed.Query, "udp_over_stream", "udp-over-stream"),
		ZeroRTTHandshake:  queryBool(parsed.Query, "zero_rtt_handshake", "zero-rtt-handshake"),
		Heartbeat:         durationFromQuery(parsed.Query, "heartbeat"),
		Network:           networkListFromQuery(parsed.Query),
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tlsOptions,
		},
	}
	return option.Outbound{Type: constant.TypeTUIC, Tag: tag, Options: options}, nil
}

func buildSSHOutbound(parsed *ParsedURI, serverOptions option.ServerOptions, tag string) (option.Outbound, error) {
	options := &option.SSHOutboundOptions{
		ServerOptions:        serverOptions,
		User:                 parsed.Username,
		Password:             parsed.Password,
		PrivateKey:           listableStringFromQuery(parsed.Query, "private_key", "private-key"),
		PrivateKeyPath:       queryFirst(parsed.Query, "private_key_path", "private-key-path"),
		PrivateKeyPassphrase: queryFirst(parsed.Query, "private_key_passphrase", "private-key-passphrase"),
		HostKey:              listableStringFromQuery(parsed.Query, "host_key", "host-key"),
		HostKeyAlgorithms:    listableStringFromQuery(parsed.Query, "host_key_algorithms", "host-key-algorithms"),
		ClientVersion:        queryFirst(parsed.Query, "client_version", "client-version"),
	}
	return option.Outbound{Type: constant.TypeSSH, Tag: tag, Options: options}, nil
}

func buildTLSOptions(query url.Values, serverName string, defaultEnabled bool) (*option.OutboundTLSOptions, error) {
	security := securityMode(query)
	enabled := defaultEnabled || security == "tls" || security == "reality"
	if !enabled || security == "none" {
		return nil, nil
	}

	tlsOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: firstNonEmpty(queryFirst(query, "sni", "servername", "server_name"), serverName),
		Insecure:   queryBool(query, "allowInsecure", "allow_insecure", "insecure", "skip-cert-verify"),
	}
	if alpn := splitCommaList(queryFirst(query, "alpn")); len(alpn) > 0 {
		tlsOptions.ALPN = badoption.Listable[string](alpn)
	}
	fingerprint := queryFirst(query, "fp", "fingerprint")
	if security == "reality" {
		fingerprint = firstNonEmpty(fingerprint, "chrome")
	}
	if fingerprint != "" {
		tlsOptions.UTLS = &option.OutboundUTLSOptions{
			Enabled:     true,
			Fingerprint: fingerprint,
		}
	}
	if security == "reality" {
		publicKey := queryFirst(query, "pbk", "publicKey", "public_key")
		if publicKey == "" {
			return nil, fmt.Errorf("%w: missing reality public key", ErrUnsupportedURI)
		}
		tlsOptions.Reality = &option.OutboundRealityOptions{
			Enabled:   true,
			PublicKey: publicKey,
			ShortID:   queryFirst(query, "sid", "shortId", "short_id"),
		}
	}
	return tlsOptions, nil
}

func networkListFromQuery(query url.Values) option.NetworkList {
	network := strings.ToLower(strings.TrimSpace(queryFirst(query, "network")))
	switch network {
	case "tcp", "udp":
		return option.NetworkList(network)
	default:
		return ""
	}
}

func durationFromQuery(query url.Values, keys ...string) badoption.Duration {
	value := queryFirst(query, keys...)
	if value == "" {
		return 0
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return badoption.Duration(parsed)
}

func intFromQuery(query url.Values, keys ...string) int {
	value := queryFirst(query, keys...)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func uint64FromQuery(query url.Values, keys ...string) uint64 {
	value := queryFirst(query, keys...)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func listableStringFromQuery(query url.Values, keys ...string) badoption.Listable[string] {
	values := splitCommaList(queryFirst(query, keys...))
	if len(values) == 0 {
		return nil
	}
	return badoption.Listable[string](values)
}

func networkBytesFromQuery(query url.Values, keys ...string) (*byteformats.NetworkBytesCompat, error) {
	value := queryFirst(query, keys...)
	if value == "" {
		return nil, nil
	}
	data, err := strconv.Unquote(`"` + strings.ReplaceAll(value, `"`, `\"`) + `"`)
	if err != nil {
		data = value
	}
	var parsed byteformats.NetworkBytesCompat
	if err := parsed.UnmarshalJSON([]byte(strconv.Quote(data))); err != nil {
		return nil, fmt.Errorf("%w: invalid bandwidth %s", ErrUnsupportedURI, value)
	}
	return &parsed, nil
}

func requiresUTLS(query url.Values) bool {
	return securityMode(query) == "reality"
}

func normalizeVLESSFlow(flow string) string {
	flow = strings.TrimSpace(flow)
	const visionFlow = "xtls-rprx-vision"
	const udp443Suffix = "-udp443"

	suffix, found := strings.CutPrefix(flow, visionFlow)
	if found {
		for suffix != "" {
			if !strings.HasPrefix(suffix, udp443Suffix) {
				return flow
			}
			suffix = strings.TrimPrefix(suffix, udp443Suffix)
		}
		return visionFlow
	}
	return flow
}

func buildV2RayTransport(query url.Values) (*option.V2RayTransportOptions, error) {
	transportType, _ := transportTypeAndTag(query)
	switch transportType {
	case "":
		return nil, nil
	case constant.V2RayTransportTypeWebsocket:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeWebsocket}
		transport.WebsocketOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		if earlyData := queryFirst(query, "ed", "maxEarlyData", "max_early_data"); earlyData != "" {
			if parsed, err := strconv.ParseUint(earlyData, 10, 32); err == nil {
				transport.WebsocketOptions.MaxEarlyData = uint32(parsed)
			}
		}
		transport.WebsocketOptions.EarlyDataHeaderName = queryFirst(
			query,
			"eh",
			"earlyDataHeaderName",
			"early_data_header_name",
		)
		if host := queryFirst(query, "host"); host != "" {
			transport.WebsocketOptions.Headers = badoption.HTTPHeader{
				"Host": badoption.Listable[string]{host},
			}
		}
		return transport, nil
	case constant.V2RayTransportTypeHTTP:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeHTTP}
		transport.HTTPOptions.Path = queryFirst(query, "path")
		if host := splitCommaList(queryFirst(query, "host")); len(host) > 0 {
			transport.HTTPOptions.Host = badoption.Listable[string](host)
		}
		return transport, nil
	case constant.V2RayTransportTypeGRPC:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeGRPC}
		transport.GRPCOptions.ServiceName = firstNonEmpty(queryFirst(query, "serviceName", "service_name"), queryFirst(query, "path"))
		return transport, nil
	case constant.V2RayTransportTypeHTTPUpgrade:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeHTTPUpgrade}
		transport.HTTPUpgradeOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		transport.HTTPUpgradeOptions.Host = queryFirst(query, "host")
		return transport, nil
	case constant.V2RayTransportTypeQUIC:
		return &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeQUIC}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported transport %s", ErrUnsupportedURI, transportType)
	}
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
