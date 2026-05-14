package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func ParseNodeURI(rawURI string) (*NodeUpsertRequest, error) {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return nil, ErrUnsupportedURI
	}

	scheme := strings.ToLower(strings.TrimSuffix(strings.SplitN(rawURI, "://", 2)[0], ":"))
	if scheme == ProtocolVMess {
		return parseVMessURI(rawURI)
	}

	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedURI, rawURI)
	}

	protocol := normalizeProtocol(parsed.Scheme)
	if !isSupportedNodeProtocol(protocol) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProtocol, parsed.Scheme)
	}

	server := strings.TrimSpace(parsed.Hostname())
	port, err := parseURLPort(parsed)
	if err != nil {
		return nil, err
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

	tags := []string{protocol}
	if transportType := normalizeTransportType(parsed.Query().Get("type")); transportType != "" {
		tags = append(tags, transportType)
	}

	return &NodeUpsertRequest{
		Name:     name,
		Protocol: protocol,
		Server:   server,
		Port:     port,
		Username: username,
		Password: password,
		RawURI:   rawURI,
		Tags:     tags,
	}, nil
}

func parseVMessURI(rawURI string) (*NodeUpsertRequest, error) {
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
	transport := normalizeTransportType(stringFromMap(data, "net"))

	tags := []string{ProtocolVMess}
	if transport != "" {
		tags = append(tags, transport)
	}

	return &NodeUpsertRequest{
		Name:     name,
		Protocol: ProtocolVMess,
		Server:   server,
		Port:     port,
		Username: stringFromMap(data, "id"),
		RawURI:   rawURI,
		Tags:     tags,
	}, nil
}

func parseURLPort(parsed *url.URL) (*uint16, error) {
	return parsePortString(parsed.Port())
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

func uriPayload(rawURI string) string {
	if _, payload, ok := strings.Cut(strings.TrimSpace(rawURI), "://"); ok {
		return strings.TrimSpace(payload)
	}
	return strings.TrimSpace(rawURI)
}
