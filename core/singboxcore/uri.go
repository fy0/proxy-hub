package singboxcore

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/byteformats"
	"github.com/sagernet/sing/common/json/badoption"
)

const (
	ProtocolHTTP        = "http"
	ProtocolSOCKS5      = "socks5"
	ProtocolShadowsocks = "shadowsocks"
	ProtocolTrojan      = "trojan"
	ProtocolVMess       = "vmess"
	ProtocolVLESS       = "vless"
	ProtocolHysteria2   = "hysteria2"
)

type ParsedURI struct {
	RawURI              string
	Name                string
	Protocol            string
	Server              string
	Port                uint16
	Username            string
	Password            string
	Query               url.Values
	VMessAlterID        int
	VMessSecurity       string
	VMessPacketEncoding string
}

func OutboundFromURI(rawURI, tag string) (option.Outbound, error) {
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
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: C.TypeVLESS,
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
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, false)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: C.TypeVMess,
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
		transport, err := buildV2RayTransport(parsed.Query)
		if err != nil {
			return option.Outbound{}, err
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
		if err != nil {
			return option.Outbound{}, err
		}
		return option.Outbound{
			Type: C.TypeTrojan,
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
			Type: C.TypeSOCKS,
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
			Type: C.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPOutboundOptions{
				ServerOptions:               serverOptions,
				Username:                    parsed.Username,
				Password:                    parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
			},
		}, nil
	case ProtocolShadowsocks:
		if strings.TrimSpace(parsed.Username) == "" || strings.TrimSpace(parsed.Password) == "" {
			return option.Outbound{}, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
		}
		return option.Outbound{
			Type: C.TypeShadowsocks,
			Tag:  tag,
			Options: &option.ShadowsocksOutboundOptions{
				ServerOptions: serverOptions,
				Method:        parsed.Username,
				Password:      parsed.Password,
				Plugin:        queryFirst(parsed.Query, "plugin"),
				PluginOptions: queryFirst(parsed.Query, "plugin_opts", "plugin-opts", "pluginOptions"),
				Network:       networkListFromQuery(parsed.Query),
			},
		}, nil
	case ProtocolHysteria2:
		if strings.TrimSpace(parsed.Password) == "" {
			return option.Outbound{}, fmt.Errorf("%w: missing hysteria2 password", ErrUnsupportedURI)
		}
		tlsOptions, err := buildTLSOptions(parsed.Query, serverOptions.Server, true)
		if err != nil {
			return option.Outbound{}, err
		}
		options := &option.Hysteria2OutboundOptions{
			ServerOptions:               serverOptions,
			ServerPorts:                 listableStringFromQuery(parsed.Query, "server_ports", "server-ports", "ports"),
			HopInterval:                 durationFromQuery(parsed.Query, "hop_interval", "hop-interval"),
			UpMbps:                      intFromQuery(parsed.Query, "up_mbps", "up-mbps", "upmbps"),
			DownMbps:                    intFromQuery(parsed.Query, "down_mbps", "down-mbps", "downmbps"),
			Password:                    parsed.Password,
			Network:                     networkListFromQuery(parsed.Query),
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
			BrutalDebug:                 queryBool(parsed.Query, "brutal_debug", "brutal-debug"),
		}
		if obfsPassword := queryFirst(parsed.Query, "obfs-password", "obfs_password", "obfsPassword"); obfsPassword != "" {
			options.Obfs = &option.Hysteria2Obfs{
				Type:     firstNonEmpty(queryFirst(parsed.Query, "obfs", "obfs-type", "obfs_type"), "salamander"),
				Password: obfsPassword,
			}
		}
		return option.Outbound{Type: C.TypeHysteria2, Tag: tag, Options: options}, nil
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func ParseURI(rawURI string) (*ParsedURI, error) {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return nil, ErrUnsupportedURI
	}
	scheme := uriScheme(rawURI)
	if scheme == ProtocolVMess {
		if parsed, err := parseVMessBase64URI(rawURI); err == nil {
			return parsed, nil
		}
	}
	if scheme == ProtocolShadowsocks {
		if parsed, err := parseShadowsocksURI(rawURI); err == nil {
			return parsed, nil
		}
	}
	return parseURLNodeURI(rawURI)
}

func parseURLNodeURI(rawURI string) (*ParsedURI, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedURI, rawURI)
	}
	protocol := normalizeProtocol(parsed.Scheme)
	if !isSupportedProtocol(protocol) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProtocol, parsed.Scheme)
	}
	server := strings.TrimSpace(parsed.Hostname())
	if server == "" {
		return nil, fmt.Errorf("%w: missing server", ErrUnsupportedURI)
	}
	port, err := parseURLPortWithDefault(parsed, protocol)
	if err != nil {
		return nil, err
	}
	query := cloneQuery(parsed.Query())
	if strings.EqualFold(parsed.Scheme, "https") {
		query.Set("security", "tls")
	}
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	switch protocol {
	case ProtocolTrojan, ProtocolHysteria2:
		password = username
		username = ""
	case ProtocolShadowsocks:
		if password == "" {
			return nil, fmt.Errorf("%w: missing shadowsocks password", ErrUnsupportedURI)
		}
	}
	vmessAlterID := 0
	vmessSecurity := ""
	vmessPacketEncoding := ""
	if protocol == ProtocolVMess {
		vmessSecurity = firstNonEmpty(queryFirst(query, "scy"), "auto")
		if security := strings.ToLower(queryFirst(query, "security")); security != "" {
			switch security {
			case "tls", "reality", "none":
			default:
				vmessSecurity = security
				query.Del("security")
			}
		}
		if value := queryFirst(query, "alterId", "alter_id", "aid"); value != "" {
			if parsed, err := strconv.Atoi(value); err == nil {
				vmessAlterID = parsed
			}
		}
		vmessPacketEncoding = queryFirst(query, "packetEncoding", "packet_encoding", "packet-encoding")
	}
	return &ParsedURI{
		RawURI:              rawURI,
		Name:                strings.TrimSpace(parsed.Fragment),
		Protocol:            protocol,
		Server:              server,
		Port:                port,
		Username:            username,
		Password:            password,
		Query:               query,
		VMessAlterID:        vmessAlterID,
		VMessSecurity:       vmessSecurity,
		VMessPacketEncoding: vmessPacketEncoding,
	}, nil
}

func parseShadowsocksURI(rawURI string) (*ParsedURI, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || normalizeProtocol(parsed.Scheme) != ProtocolShadowsocks {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedURI, rawURI)
	}
	name := strings.TrimSpace(parsed.Fragment)
	query := cloneQuery(parsed.Query())
	server := strings.TrimSpace(parsed.Hostname())
	if server != "" && (parsed.Port() != "" || parsed.User != nil) {
		port, err := parseURLPortWithDefault(parsed, ProtocolShadowsocks)
		if err != nil {
			return nil, err
		}
		method := parsed.User.Username()
		password, _ := parsed.User.Password()
		if method != "" && password == "" {
			if decoded, err := decodeBase64Flexible(method); err == nil {
				if decodedMethod, decodedPassword, ok := strings.Cut(string(decoded), ":"); ok {
					method = decodedMethod
					password = decodedPassword
				}
			}
		}
		if strings.TrimSpace(method) == "" || strings.TrimSpace(password) == "" {
			return nil, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
		}
		return &ParsedURI{
			RawURI:   rawURI,
			Name:     name,
			Protocol: ProtocolShadowsocks,
			Server:   server,
			Port:     port,
			Username: strings.TrimSpace(method),
			Password: strings.TrimSpace(password),
			Query:    query,
		}, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(rawURI, parsed.Scheme+"://"))
	if hashIndex := strings.Index(payload, "#"); hashIndex >= 0 {
		payload = payload[:hashIndex]
	}
	if queryIndex := strings.Index(payload, "?"); queryIndex >= 0 {
		payload = payload[:queryIndex]
	}
	decoded, err := decodeBase64Flexible(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid shadowsocks payload", ErrUnsupportedURI)
	}
	legacyURL, err := url.Parse("ss://" + strings.TrimSpace(string(decoded)))
	if err != nil || legacyURL.Hostname() == "" {
		return nil, fmt.Errorf("%w: invalid shadowsocks payload", ErrUnsupportedURI)
	}
	port, err := parseURLPortWithDefault(legacyURL, ProtocolShadowsocks)
	if err != nil {
		return nil, err
	}
	method := legacyURL.User.Username()
	password, _ := legacyURL.User.Password()
	if method == "" || password == "" {
		return nil, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
	}
	return &ParsedURI{
		RawURI:   rawURI,
		Name:     name,
		Protocol: ProtocolShadowsocks,
		Server:   strings.TrimSpace(legacyURL.Hostname()),
		Port:     port,
		Username: strings.TrimSpace(method),
		Password: strings.TrimSpace(password),
		Query:    query,
	}, nil
}

func parseVMessBase64URI(rawURI string) (*ParsedURI, error) {
	payload := uriPayload(rawURI)
	content, err := decodeBase64Flexible(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid vmess payload", ErrUnsupportedURI)
	}
	var data map[string]any
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("%w: invalid vmess json", ErrUnsupportedURI)
	}
	server := stringFromMap(data, "add")
	port, err := parsePortString(stringFromMap(data, "port"))
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	for key, dataKey := range map[string]string{
		"type":           "net",
		"host":           "host",
		"path":           "path",
		"sni":            "sni",
		"alpn":           "alpn",
		"fp":             "fp",
		"packetEncoding": "packet_encoding",
	} {
		if value := stringFromMap(data, dataKey); value != "" {
			query.Set(key, value)
		}
	}
	if value := stringFromMap(data, "packetEncoding"); value != "" {
		query.Set("packetEncoding", value)
	}
	if tlsMode := stringFromMap(data, "tls"); tlsMode != "" {
		query.Set("security", tlsMode)
	}
	alterID := 0
	if value := stringFromMap(data, "aid"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			alterID = parsed
		}
	}
	return &ParsedURI{
		RawURI:              rawURI,
		Name:                stringFromMap(data, "ps"),
		Protocol:            ProtocolVMess,
		Server:              server,
		Port:                port,
		Username:            stringFromMap(data, "id"),
		Query:               query,
		VMessAlterID:        alterID,
		VMessSecurity:       firstNonEmpty(stringFromMap(data, "scy"), "auto"),
		VMessPacketEncoding: queryFirst(query, "packetEncoding", "packet_encoding", "packet-encoding"),
	}, nil
}

func parseURLPortWithDefault(parsed *url.URL, protocol string) (uint16, error) {
	if parsed.Port() != "" {
		return parsePortString(parsed.Port())
	}
	switch protocol {
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan, ProtocolHysteria2:
		return 443, nil
	case ProtocolHTTP:
		if strings.EqualFold(parsed.Scheme, "https") {
			return 443, nil
		}
		return 80, nil
	case ProtocolSOCKS5:
		return 1080, nil
	default:
		return 0, ErrInvalidPort
	}
}

func parsePortString(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 16)
	if err != nil || parsed == 0 {
		return 0, ErrInvalidPort
	}
	return uint16(parsed), nil
}

func decodeBase64Flexible(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(payload); err == nil {
			return decoded, nil
		}
	}
	if missing := len(payload) % 4; missing != 0 {
		return base64.StdEncoding.DecodeString(payload + strings.Repeat("=", 4-missing))
	}
	return nil, ErrUnsupportedURI
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

func buildV2RayTransport(query url.Values) (*option.V2RayTransportOptions, error) {
	switch transportType := transportType(query); transportType {
	case "":
		return nil, nil
	case C.V2RayTransportTypeWebsocket:
		transport := &option.V2RayTransportOptions{Type: C.V2RayTransportTypeWebsocket}
		transport.WebsocketOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		if host := queryFirst(query, "host"); host != "" {
			transport.WebsocketOptions.Headers = badoption.HTTPHeader{
				"Host": badoption.Listable[string]{host},
			}
		}
		return transport, nil
	case C.V2RayTransportTypeHTTP:
		transport := &option.V2RayTransportOptions{Type: C.V2RayTransportTypeHTTP}
		transport.HTTPOptions.Path = queryFirst(query, "path")
		if host := splitCommaList(queryFirst(query, "host")); len(host) > 0 {
			transport.HTTPOptions.Host = badoption.Listable[string](host)
		}
		return transport, nil
	case C.V2RayTransportTypeGRPC:
		transport := &option.V2RayTransportOptions{Type: C.V2RayTransportTypeGRPC}
		transport.GRPCOptions.ServiceName = firstNonEmpty(queryFirst(query, "serviceName", "service_name"), queryFirst(query, "path"))
		return transport, nil
	case C.V2RayTransportTypeHTTPUpgrade:
		transport := &option.V2RayTransportOptions{Type: C.V2RayTransportTypeHTTPUpgrade}
		transport.HTTPUpgradeOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		transport.HTTPUpgradeOptions.Host = queryFirst(query, "host")
		return transport, nil
	case C.V2RayTransportTypeQUIC:
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeQUIC}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported transport %s", ErrUnsupportedURI, transportType)
	}
}

func normalizeProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ":")))
	switch value {
	case "socks", "socks5", "socks5h":
		return ProtocolSOCKS5
	case "ss", "shadowsocks":
		return ProtocolShadowsocks
	case "hy2":
		return ProtocolHysteria2
	case "https":
		return ProtocolHTTP
	default:
		return value
	}
}

func isSupportedProtocol(protocol string) bool {
	switch protocol {
	case ProtocolHTTP, ProtocolSOCKS5, ProtocolShadowsocks, ProtocolTrojan, ProtocolVMess, ProtocolVLESS, ProtocolHysteria2:
		return true
	default:
		return false
	}
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
	var parsed byteformats.NetworkBytesCompat
	if err := parsed.UnmarshalJSON([]byte(strconv.Quote(value))); err != nil {
		return nil, fmt.Errorf("%w: invalid bandwidth %s", ErrUnsupportedURI, value)
	}
	return &parsed, nil
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

func securityMode(query url.Values) string {
	security := strings.ToLower(strings.TrimSpace(queryFirst(query, "security")))
	if security != "" {
		return security
	}
	tls := strings.ToLower(strings.TrimSpace(queryFirst(query, "tls")))
	switch tls {
	case "1", "true", "yes", "y", "tls":
		return "tls"
	case "0", "false", "no", "n", "none":
		return "none"
	default:
		return tls
	}
}

func transportType(query url.Values) string {
	raw := strings.ToLower(strings.TrimSpace(queryFirst(query, "type", "network")))
	switch raw {
	case "", "tcp", "raw", "none":
		return ""
	case "websocket":
		return "ws"
	case "h2":
		return "http"
	default:
		return raw
	}
}

func queryBool(query url.Values, keys ...string) bool {
	value := queryFirst(query, keys...)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func queryFirst(query url.Values, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func cloneQuery(query url.Values) url.Values {
	clone := make(url.Values, len(query))
	for key, values := range query {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func splitCommaList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func uriScheme(rawURI string) string {
	rawURI = strings.TrimSpace(rawURI)
	if scheme, _, ok := strings.Cut(rawURI, "://"); ok {
		return normalizeProtocol(scheme)
	}
	if scheme, _, ok := strings.Cut(rawURI, ":"); ok {
		return normalizeProtocol(scheme)
	}
	return ""
}

func uriPayload(rawURI string) string {
	if _, payload, ok := strings.Cut(strings.TrimSpace(rawURI), "://"); ok {
		return strings.TrimSpace(payload)
	}
	return strings.TrimSpace(rawURI)
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
