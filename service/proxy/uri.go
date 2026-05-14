package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type parsedNodeURI struct {
	RawURI              string
	Name                string
	Protocol            string
	Server              string
	Port                *uint16
	Username            string
	Password            string
	Query               url.Values
	Tags                []string
	VMessAlterID        int
	VMessSecurity       string
	VMessPacketEncoding string
}

func ParseNodeURI(rawURI string) (*NodeUpsertRequest, error) {
	parsed, err := parseNodeURI(rawURI)
	if err != nil {
		return nil, err
	}
	return parsed.toNodeUpsertRequest(), nil
}

func parseNodeURI(rawURI string) (*parsedNodeURI, error) {
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

	return parseURLNodeURI(rawURI)
}

func parseURLNodeURI(rawURI string) (*parsedNodeURI, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedURI, rawURI)
	}

	protocol := normalizeProtocol(parsed.Scheme)
	if !isSupportedNodeProtocol(protocol) {
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
	if protocol == ProtocolTrojan {
		password = username
		username = ""
	}

	name := strings.TrimSpace(parsed.Fragment)
	if name == "" {
		name = defaultNodeName(protocol, server)
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

	return &parsedNodeURI{
		RawURI:              rawURI,
		Name:                name,
		Protocol:            protocol,
		Server:              server,
		Port:                port,
		Username:            username,
		Password:            password,
		Query:               query,
		Tags:                tagsForParsedNode(protocol, query),
		VMessAlterID:        vmessAlterID,
		VMessSecurity:       vmessSecurity,
		VMessPacketEncoding: vmessPacketEncoding,
	}, nil
}

func parseVMessURI(rawURI string) (*NodeUpsertRequest, error) {
	parsed, err := parseVMessBase64URI(rawURI)
	if err != nil {
		return nil, err
	}
	return parsed.toNodeUpsertRequest(), nil
}

func parseVMessBase64URI(rawURI string) (*parsedNodeURI, error) {
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
	name := firstNonEmpty(stringFromMap(data, "ps"), defaultNodeName(ProtocolVMess, server))
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

	return &parsedNodeURI{
		RawURI:              rawURI,
		Name:                name,
		Protocol:            ProtocolVMess,
		Server:              server,
		Port:                port,
		Username:            stringFromMap(data, "id"),
		Query:               query,
		Tags:                tagsForParsedNode(ProtocolVMess, query),
		VMessAlterID:        alterID,
		VMessSecurity:       firstNonEmpty(stringFromMap(data, "scy"), "auto"),
		VMessPacketEncoding: queryFirst(query, "packetEncoding", "packet_encoding", "packet-encoding"),
	}, nil
}

func parseURLPortWithDefault(parsed *url.URL, protocol string) (*uint16, error) {
	if parsed.Port() != "" {
		return parsePortString(parsed.Port())
	}

	var port uint16
	switch protocol {
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan:
		port = 443
	case ProtocolHTTP:
		if strings.EqualFold(parsed.Scheme, "https") {
			port = 443
		} else {
			port = 80
		}
	case ProtocolSOCKS5:
		port = 1080
	default:
		return nil, ErrInvalidPort
	}
	return &port, nil
}

func parsePortString(value string) (*uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrInvalidPort
	}
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return nil, ErrInvalidPort
	}
	port := uint16(parsed)
	return &port, nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeTransportType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "tcp", "raw", "none":
		return ""
	case "websocket":
		return "ws"
	case "h2":
		return "http"
	case "ws", "http", "grpc", "quic", "httpupgrade":
		return value
	default:
		return value
	}
}

func normalizeTransportTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "tcp", "raw", "none":
		return ""
	case "websocket":
		return "ws"
	case "h2":
		return "h2"
	case "ws", "http", "grpc", "quic", "httpupgrade":
		return value
	default:
		return value
	}
}

func transportTypeAndTag(query url.Values) (string, string) {
	raw := queryFirst(query, "type", "network")
	return normalizeTransportType(raw), normalizeTransportTag(raw)
}

func tagsForParsedNode(protocol string, query url.Values) []string {
	tags := []string{protocol}
	if _, transportTag := transportTypeAndTag(query); transportTag != "" {
		tags = append(tags, transportTag)
	}
	switch securityMode(query) {
	case "tls", "reality":
		tags = append(tags, securityMode(query))
	}
	return uniqueNonEmpty(tags)
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

func splitCommaList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	return uniqueNonEmpty(parts)
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

func uriScheme(rawURI string) string {
	rawURI = strings.TrimSpace(rawURI)
	if scheme, _, ok := strings.Cut(rawURI, "://"); ok {
		return normalizeProtocol(scheme)
	}
	if scheme, _, ok := strings.Cut(rawURI, ":"); ok {
		return normalizeProtocol(scheme)
	}
	return ProtocolUnknown
}

func uriPayload(rawURI string) string {
	if _, payload, ok := strings.Cut(strings.TrimSpace(rawURI), "://"); ok {
		return strings.TrimSpace(payload)
	}
	return strings.TrimSpace(rawURI)
}

func (parsed *parsedNodeURI) toNodeUpsertRequest() *NodeUpsertRequest {
	if parsed == nil {
		return nil
	}
	return &NodeUpsertRequest{
		Name:     parsed.Name,
		Protocol: parsed.Protocol,
		Server:   parsed.Server,
		Port:     parsed.Port,
		Username: parsed.Username,
		Password: parsed.Password,
		RawURI:   parsed.RawURI,
		Tags:     append([]string(nil), parsed.Tags...),
	}
}

func expandImportValue(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if uris := clashProxyURIs(value); len(uris) > 0 {
		return uris
	}
	if strings.ContainsAny(value, "\r\n") {
		values := make([]string, 0)
		for _, line := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '\n' || r == '\r'
		}) {
			values = append(values, expandImportValue(line)...)
		}
		return values
	}
	if strings.Contains(value, "://") {
		return []string{value}
	}
	decoded, err := decodeBase64Flexible(value)
	if err != nil {
		return []string{value}
	}
	decodedText := strings.TrimSpace(string(decoded))
	if decodedText == "" {
		return []string{value}
	}
	return expandImportValue(decodedText)
}

type clashConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

func clashProxyURIs(raw string) []string {
	if !strings.Contains(raw, "proxies:") {
		return nil
	}
	var config clashConfig
	if err := yaml.Unmarshal([]byte(raw), &config); err != nil {
		return nil
	}
	uris := make([]string, 0, len(config.Proxies))
	for _, proxy := range config.Proxies {
		if uri := clashProxyToURI(proxy); uri != "" {
			uris = append(uris, uri)
		}
	}
	return uris
}

func clashProxyToURI(proxy map[string]any) string {
	protocol := normalizeProtocol(stringFromMap(proxy, "type"))
	switch protocol {
	case ProtocolVLESS:
		return clashVLESSURI(proxy)
	case ProtocolVMess:
		return clashVMessURI(proxy)
	case ProtocolTrojan:
		return clashTrojanURI(proxy)
	case ProtocolSOCKS5:
		return clashSimpleURI(proxy, ProtocolSOCKS5)
	case ProtocolHTTP:
		return clashSimpleURI(proxy, ProtocolHTTP)
	default:
		return ""
	}
}

func clashVLESSURI(proxy map[string]any) string {
	server, port := clashServerPort(proxy)
	uuid := firstNonEmpty(stringFromMap(proxy, "uuid"), stringFromMap(proxy, "id"))
	if server == "" || port == "" || uuid == "" {
		return ""
	}
	query := clashV2RayQuery(proxy)
	query.Set("encryption", firstNonEmpty(stringFromMap(proxy, "encryption"), "none"))
	if flow := stringFromMap(proxy, "flow"); flow != "" {
		query.Set("flow", flow)
	}
	u := url.URL{
		Scheme:   ProtocolVLESS,
		User:     url.User(uuid),
		Host:     net.JoinHostPort(server, port),
		RawQuery: query.Encode(),
		Fragment: stringFromMap(proxy, "name"),
	}
	return u.String()
}

func clashTrojanURI(proxy map[string]any) string {
	server, port := clashServerPort(proxy)
	password := stringFromMap(proxy, "password")
	if server == "" || port == "" || password == "" {
		return ""
	}
	query := clashV2RayQuery(proxy)
	if value, ok := boolFromMap(proxy, "tls"); ok && !value {
		query.Set("security", "none")
	}
	u := url.URL{
		Scheme:   ProtocolTrojan,
		User:     url.User(password),
		Host:     net.JoinHostPort(server, port),
		RawQuery: query.Encode(),
		Fragment: stringFromMap(proxy, "name"),
	}
	return u.String()
}

func clashVMessURI(proxy map[string]any) string {
	server, port := clashServerPort(proxy)
	uuid := firstNonEmpty(stringFromMap(proxy, "uuid"), stringFromMap(proxy, "id"))
	if server == "" || port == "" || uuid == "" {
		return ""
	}
	network := firstNonEmpty(stringFromMap(proxy, "network"), "tcp")
	host, path := clashTransportHostPath(proxy, network)
	payload := map[string]string{
		"v":    "2",
		"ps":   stringFromMap(proxy, "name"),
		"add":  server,
		"port": port,
		"id":   uuid,
		"aid":  firstNonEmpty(stringFromMap(proxy, "alterId"), stringFromMap(proxy, "alter-id"), "0"),
		"scy":  firstNonEmpty(stringFromMap(proxy, "cipher"), "auto"),
		"net":  network,
		"type": firstNonEmpty(stringFromMap(proxy, "network-type"), "none"),
		"host": host,
		"path": path,
		"tls":  clashTLSMode(proxy),
		"sni":  firstNonEmpty(stringFromMap(proxy, "servername"), stringFromMap(proxy, "sni")),
		"alpn": stringListFromMap(proxy, "alpn"),
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return "vmess://" + base64.RawStdEncoding.EncodeToString(content)
}

func clashSimpleURI(proxy map[string]any, protocol string) string {
	server, port := clashServerPort(proxy)
	if server == "" || port == "" {
		return ""
	}
	scheme := protocol
	if protocol == ProtocolHTTP {
		if value, ok := boolFromMap(proxy, "tls"); ok && value {
			scheme = "https"
		}
	}
	u := url.URL{
		Scheme:   scheme,
		Host:     net.JoinHostPort(server, port),
		Fragment: stringFromMap(proxy, "name"),
	}
	username := stringFromMap(proxy, "username")
	password := stringFromMap(proxy, "password")
	if username != "" && password != "" {
		u.User = url.UserPassword(username, password)
	} else if username != "" {
		u.User = url.User(username)
	}
	return u.String()
}

func clashV2RayQuery(proxy map[string]any) url.Values {
	query := url.Values{}
	if network := stringFromMap(proxy, "network"); network != "" {
		query.Set("type", network)
	}
	if security := clashTLSMode(proxy); security != "" {
		query.Set("security", security)
	}
	if sni := firstNonEmpty(stringFromMap(proxy, "servername"), stringFromMap(proxy, "sni")); sni != "" {
		query.Set("sni", sni)
	}
	if fingerprint := firstNonEmpty(stringFromMap(proxy, "client-fingerprint"), stringFromMap(proxy, "fingerprint"), stringFromMap(proxy, "fp")); fingerprint != "" {
		query.Set("fp", fingerprint)
	}
	if alpn := stringListFromMap(proxy, "alpn"); alpn != "" {
		query.Set("alpn", alpn)
	}
	if value, ok := boolFromMap(proxy, "skip-cert-verify"); ok && value {
		query.Set("allowInsecure", "true")
	}
	if pbk, sid := clashRealityOptions(proxy); pbk != "" || sid != "" {
		query.Set("security", "reality")
		if pbk != "" {
			query.Set("pbk", pbk)
		}
		if sid != "" {
			query.Set("sid", sid)
		}
	}
	network := query.Get("type")
	host, path := clashTransportHostPath(proxy, network)
	if host != "" {
		query.Set("host", host)
	}
	if path != "" {
		query.Set("path", path)
	}
	if serviceName := clashGRPCServiceName(proxy); serviceName != "" {
		query.Set("serviceName", serviceName)
	}
	return query
}

func clashServerPort(proxy map[string]any) (string, string) {
	server := stringFromMap(proxy, "server")
	port := stringFromMap(proxy, "port")
	if server == "" || port == "" {
		return "", ""
	}
	if _, err := parsePortString(port); err != nil {
		return "", ""
	}
	return server, port
}

func clashTLSMode(proxy map[string]any) string {
	if security := strings.ToLower(strings.TrimSpace(stringFromMap(proxy, "security"))); security != "" {
		return security
	}
	if value, ok := boolFromMap(proxy, "reality"); ok && value {
		return "reality"
	}
	if value, ok := boolFromMap(proxy, "tls"); ok && value {
		return "tls"
	}
	return ""
}

func clashRealityOptions(proxy map[string]any) (string, string) {
	options := mapFromMap(proxy, "reality-opts", "reality_opts", "reality")
	return firstNonEmpty(
			stringFromMap(options, "public-key"),
			stringFromMap(options, "public_key"),
			stringFromMap(proxy, "pbk"),
			stringFromMap(proxy, "public-key"),
		), firstNonEmpty(
			stringFromMap(options, "short-id"),
			stringFromMap(options, "short_id"),
			stringFromMap(proxy, "sid"),
			stringFromMap(proxy, "short-id"),
		)
}

func clashTransportHostPath(proxy map[string]any, network string) (string, string) {
	switch normalizeTransportTag(network) {
	case "ws":
		options := mapFromMap(proxy, "ws-opts", "ws_opts")
		headers := mapFromMap(options, "headers")
		return firstNonEmpty(
				stringFromMap(headers, "Host"),
				stringFromMap(headers, "host"),
				stringFromMap(options, "host"),
			),
			stringFromMap(options, "path")
	case "h2", "http":
		options := mapFromMap(proxy, "h2-opts", "h2_opts", "http-opts", "http_opts")
		return stringListFromMap(options, "host"), stringFromMap(options, "path")
	default:
		return stringFromMap(proxy, "host"), stringFromMap(proxy, "path")
	}
}

func clashGRPCServiceName(proxy map[string]any) string {
	options := mapFromMap(proxy, "grpc-opts", "grpc_opts")
	return firstNonEmpty(
		stringFromMap(options, "grpc-service-name"),
		stringFromMap(options, "grpc_service_name"),
		stringFromMap(options, "serviceName"),
		stringFromMap(options, "service-name"),
	)
}

func mapFromMap(values map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(map[string]any); ok {
			return typed
		}
		if typed, ok := value.(map[any]any); ok {
			result := make(map[string]any, len(typed))
			for nestedKey, nestedValue := range typed {
				result[strings.TrimSpace(fmt.Sprint(nestedKey))] = nestedValue
			}
			return result
		}
	}
	return map[string]any{}
}

func boolFromMap(values map[string]any, key string) (bool, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "y":
			return true, true
		case "0", "false", "no", "n":
			return false, true
		}
	}
	return false, false
}

func stringListFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ",")
	default:
		return stringFromMap(values, key)
	}
}
