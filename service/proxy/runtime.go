package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	adapterInbound "github.com/sagernet/sing-box/adapter/inbound"
	adapterOutbound "github.com/sagernet/sing-box/adapter/outbound"
	adapterService "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/block"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/group"
	protocolHTTP "github.com/sagernet/sing-box/protocol/http"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"
	"go.uber.org/zap"

	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"
)

const urlTestLink = "https://www.gstatic.com/generate_204"

type RuntimeInbound struct {
	MappingID string `json:"mappingId"`
	Tag       string `json:"tag"`
	Listen    string `json:"listen"`
	Outbound  string `json:"outbound"`
}

type RuntimeStatus struct {
	Running   bool             `json:"running"`
	State     string           `json:"state"`
	Error     string           `json:"error,omitempty"`
	Inbounds  []RuntimeInbound `json:"inbounds"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

type runtimeManager struct {
	mu       sync.Mutex
	instance *box.Box
	status   RuntimeStatus
}

var singBoxRuntime = &runtimeManager{
	status: RuntimeStatus{
		State:     "stopped",
		UpdatedAt: time.Now(),
	},
}

func RuntimeStatusGet() RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	status := singBoxRuntime.status
	status.Inbounds = append([]RuntimeInbound(nil), singBoxRuntime.status.Inbounds...)
	return status
}

func RuntimeReload(ctx context.Context) (RuntimeStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	options, inbounds, err := BuildSingBoxOptions(ctx, nil)
	if err != nil {
		status := setRuntimeError(err)
		return status, err
	}

	singBoxRuntime.mu.Lock()
	old := singBoxRuntime.instance
	singBoxRuntime.instance = nil
	singBoxRuntime.status = RuntimeStatus{
		Running:   false,
		State:     "reloading",
		Inbounds:  inbounds,
		UpdatedAt: time.Now(),
	}
	singBoxRuntime.mu.Unlock()

	if old != nil {
		if closeErr := old.Close(); closeErr != nil {
			utils.Logger.Warn("关闭旧 sing-box 实例失败", zap.Error(closeErr))
		}
	}

	if len(inbounds) == 0 {
		return setRuntimeStatus(RuntimeStatus{
			Running:   false,
			State:     "stopped",
			Inbounds:  []RuntimeInbound{},
			UpdatedAt: time.Now(),
		}), nil
	}

	instance, err := box.New(box.Options{
		Options: options,
		Context: singBoxContext(context.Background()),
	})
	if err != nil {
		status := setRuntimeError(err)
		return status, err
	}
	if err := instance.Start(); err != nil {
		_ = instance.Close()
		status := setRuntimeError(err)
		return status, err
	}

	return setRuntimeInstance(instance, RuntimeStatus{
		Running:   true,
		State:     "running",
		Inbounds:  inbounds,
		UpdatedAt: time.Now(),
	}), nil
}

func RuntimeStop() error {
	singBoxRuntime.mu.Lock()
	instance := singBoxRuntime.instance
	singBoxRuntime.instance = nil
	singBoxRuntime.status = RuntimeStatus{
		Running:   false,
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		UpdatedAt: time.Now(),
	}
	singBoxRuntime.mu.Unlock()

	if instance != nil {
		return instance.Close()
	}
	return nil
}

func BuildSingBoxOptions(ctx context.Context, tx model.DBTx) (option.Options, []RuntimeInbound, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var mappings []*tables.PortMappingTable
	if err := tx.Where("enabled = ?", true).Order(mappingOrderClause()).Find(&mappings).Error; err != nil {
		return option.Options{}, nil, err
	}

	outbounds := []option.Outbound{
		{
			Type:    constant.TypeDirect,
			Tag:     constant.TypeDirect,
			Options: &option.DirectOutboundOptions{},
		},
		{
			Type:    constant.TypeBlock,
			Tag:     constant.TypeBlock,
			Options: &option.StubOptions{},
		},
	}
	outboundTags := map[string]struct{}{
		constant.TypeDirect: {},
		constant.TypeBlock:  {},
	}
	inbounds := make([]option.Inbound, 0, len(mappings))
	rules := make([]option.Rule, 0, len(mappings))
	statusInbounds := make([]RuntimeInbound, 0, len(mappings))

	for _, mapping := range mappings {
		nodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(mapping.NodeIDsJSON))
		if err != nil {
			return option.Options{}, nil, err
		}

		nodeTags := make([]string, 0, len(nodes))
		for _, node := range nodes {
			tag := nodeOutboundTag(node.ID)
			nodeTags = append(nodeTags, tag)
			if _, exists := outboundTags[tag]; exists {
				continue
			}
			outbound, err := buildNodeOutbound(node, tag)
			if err != nil {
				return option.Options{}, nil, fmt.Errorf("节点 %s 配置无效: %w", node.Name, err)
			}
			outbounds = append(outbounds, outbound)
			outboundTags[tag] = struct{}{}
		}

		routeTag, groupOutbound := buildMappingOutbound(mapping, nodeTags)
		if groupOutbound != nil {
			if _, exists := outboundTags[routeTag]; !exists {
				outbounds = append(outbounds, *groupOutbound)
				outboundTags[routeTag] = struct{}{}
			}
		}

		inbound, err := buildMappingInbound(mapping)
		if err != nil {
			return option.Options{}, nil, err
		}
		inbounds = append(inbounds, inbound)
		rules = append(rules, buildInboundRouteRule(inbound.Tag, routeTag))
		statusInbounds = append(statusInbounds, RuntimeInbound{
			MappingID: mapping.ID,
			Tag:       inbound.Tag,
			Listen:    fmt.Sprintf("%s:%d", mapping.ListenAddress, mapping.ListenPort),
			Outbound:  routeTag,
		})
	}

	options := option.Options{
		Log: &option.LogOptions{
			Level:        "warn",
			Timestamp:    true,
			DisableColor: true,
		},
		Inbounds:  inbounds,
		Outbounds: outbounds,
		Route: &option.RouteOptions{
			Rules: rules,
			Final: constant.TypeDirect,
		},
	}
	return options, statusInbounds, nil
}

func singBoxContext(ctx context.Context) context.Context {
	inboundRegistry := adapterInbound.NewRegistry()
	socks.RegisterInbound(inboundRegistry)
	protocolHTTP.RegisterInbound(inboundRegistry)
	mixed.RegisterInbound(inboundRegistry)

	outboundRegistry := adapterOutbound.NewRegistry()
	block.RegisterOutbound(outboundRegistry)
	direct.RegisterOutbound(outboundRegistry)
	group.RegisterSelector(outboundRegistry)
	group.RegisterURLTest(outboundRegistry)
	socks.RegisterOutbound(outboundRegistry)
	protocolHTTP.RegisterOutbound(outboundRegistry)
	vmess.RegisterOutbound(outboundRegistry)
	trojan.RegisterOutbound(outboundRegistry)
	vless.RegisterOutbound(outboundRegistry)

	dnsRegistry := dns.NewTransportRegistry()
	local.RegisterTransport(dnsRegistry)

	return box.Context(
		ctx,
		inboundRegistry,
		outboundRegistry,
		endpoint.NewRegistry(),
		dnsRegistry,
		adapterService.NewRegistry(),
	)
}

func buildMappingInbound(mapping *tables.PortMappingTable) (option.Inbound, error) {
	listen, err := parseListenAddr(mapping.ListenAddress)
	if err != nil {
		return option.Inbound{}, err
	}

	listenOptions := option.ListenOptions{
		Listen:     listen,
		ListenPort: mapping.ListenPort,
	}
	users := inboundUsers(mapping.Username, mapping.Password)
	tag := mappingInboundTag(mapping.ID)

	switch normalizeOutboundProtocol(mapping.OutboundProtocol) {
	case OutboundProtocolSOCKS:
		return option.Inbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SocksInboundOptions{
				ListenOptions: listenOptions,
				Users:         users,
			},
		}, nil
	case OutboundProtocolHTTP:
		return option.Inbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPMixedInboundOptions{
				ListenOptions: listenOptions,
				Users:         users,
			},
		}, nil
	default:
		return option.Inbound{
			Type: constant.TypeMixed,
			Tag:  tag,
			Options: &option.HTTPMixedInboundOptions{
				ListenOptions: listenOptions,
				Users:         users,
			},
		}, nil
	}
}

func buildMappingOutbound(mapping *tables.PortMappingTable, nodeTags []string) (string, *option.Outbound) {
	if len(nodeTags) == 0 {
		return constant.TypeBlock, nil
	}
	if len(nodeTags) == 1 {
		return nodeTags[0], nil
	}

	activeTag := ""
	if mapping.ActiveNodeID != "" {
		activeTag = nodeOutboundTag(mapping.ActiveNodeID)
	}
	if activeTag == "" || !containsString(nodeTags, activeTag) {
		activeTag = nodeTags[0]
	}

	groupTag := mappingOutboundTag(mapping.ID)
	switch normalizeStrategy(mapping.Strategy) {
	case StrategyFailover, StrategyLoadBalance:
		return groupTag, &option.Outbound{
			Type: constant.TypeURLTest,
			Tag:  groupTag,
			Options: &option.URLTestOutboundOptions{
				Outbounds:   nodeTags,
				URL:         urlTestLink,
				Interval:    badoption.Duration(3 * time.Minute),
				IdleTimeout: badoption.Duration(30 * time.Minute),
			},
		}
	default:
		return groupTag, &option.Outbound{
			Type: constant.TypeSelector,
			Tag:  groupTag,
			Options: &option.SelectorOutboundOptions{
				Outbounds: nodeTags,
				Default:   activeTag,
			},
		}
	}
}

func buildInboundRouteRule(inboundTag, outboundTag string) option.Rule {
	return option.Rule{
		Type: constant.RuleTypeDefault,
		DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{
				Inbound: badoption.Listable[string]{inboundTag},
			},
			RuleAction: option.RuleAction{
				Action: constant.RuleActionTypeRoute,
				RouteOptions: option.RouteActionOptions{
					Outbound: outboundTag,
				},
			},
		},
	}
}

func buildNodeOutbound(node *tables.ProxyNodeTable, tag string) (option.Outbound, error) {
	if strings.TrimSpace(node.RawURI) != "" {
		outbound, err := buildNodeOutboundFromURI(node.RawURI, tag)
		if err == nil {
			return outbound, nil
		}
	}

	if node.Port == nil || *node.Port == 0 {
		return option.Outbound{}, ErrInvalidPort
	}
	serverOptions := option.ServerOptions{
		Server:     node.Server,
		ServerPort: *node.Port,
	}
	switch normalizeProtocol(node.Protocol) {
	case ProtocolVLESS:
		return option.Outbound{
			Type: constant.TypeVLESS,
			Tag:  tag,
			Options: &option.VLESSOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          node.Username,
			},
		}, nil
	case ProtocolVMess:
		return option.Outbound{
			Type: constant.TypeVMess,
			Tag:  tag,
			Options: &option.VMessOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          node.Username,
				Security:      "auto",
			},
		}, nil
	case ProtocolTrojan:
		return option.Outbound{
			Type: constant.TypeTrojan,
			Tag:  tag,
			Options: &option.TrojanOutboundOptions{
				ServerOptions: serverOptions,
				Password:      node.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: &option.OutboundTLSOptions{Enabled: true},
				},
			},
		}, nil
	case ProtocolSOCKS5:
		return option.Outbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SOCKSOutboundOptions{
				ServerOptions: serverOptions,
				Version:       "5",
				Username:      node.Username,
				Password:      node.Password,
			},
		}, nil
	case ProtocolHTTP:
		return option.Outbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPOutboundOptions{
				ServerOptions: serverOptions,
				Username:      node.Username,
				Password:      node.Password,
			},
		}, nil
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func buildNodeOutboundFromURI(rawURI string, tag string) (option.Outbound, error) {
	rawURI = strings.TrimSpace(rawURI)
	if strings.HasPrefix(strings.ToLower(rawURI), "vmess://") {
		return buildVMessOutbound(rawURI, tag)
	}

	parsed, err := url.Parse(rawURI)
	if err != nil {
		return option.Outbound{}, err
	}
	port, err := parseURLPort(parsed)
	if err != nil {
		return option.Outbound{}, err
	}
	serverOptions := option.ServerOptions{
		Server:     parsed.Hostname(),
		ServerPort: *port,
	}
	query := parsed.Query()
	protocol := normalizeProtocol(parsed.Scheme)
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	if requiresUTLS(query) && !withUTLS {
		return option.Outbound{}, ErrUTLSRequired
	}

	switch protocol {
	case ProtocolVLESS:
		return option.Outbound{
			Type: constant.TypeVLESS,
			Tag:  tag,
			Options: &option.VLESSOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          username,
				Flow:          queryFirst(query, "flow"),
				PacketEncoding: stringPtrOrNil(
					queryFirst(query, "packetEncoding", "packet_encoding", "packet-encoding"),
				),
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: buildTLSOptions(query, serverOptions.Server, false),
				},
				Transport: buildV2RayTransport(query),
			},
		}, nil
	case ProtocolTrojan:
		return option.Outbound{
			Type: constant.TypeTrojan,
			Tag:  tag,
			Options: &option.TrojanOutboundOptions{
				ServerOptions: serverOptions,
				Password:      username,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: buildTLSOptions(query, serverOptions.Server, true),
				},
				Transport: buildV2RayTransport(query),
			},
		}, nil
	case ProtocolSOCKS5:
		return option.Outbound{
			Type: constant.TypeSOCKS,
			Tag:  tag,
			Options: &option.SOCKSOutboundOptions{
				ServerOptions: serverOptions,
				Version:       "5",
				Username:      username,
				Password:      password,
			},
		}, nil
	case ProtocolHTTP:
		return option.Outbound{
			Type: constant.TypeHTTP,
			Tag:  tag,
			Options: &option.HTTPOutboundOptions{
				ServerOptions: serverOptions,
				Username:      username,
				Password:      password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: buildTLSOptions(query, serverOptions.Server, false),
				},
			},
		}, nil
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func buildVMessOutbound(rawURI string, tag string) (option.Outbound, error) {
	content, err := decodeBase64Flexible(uriPayload(rawURI))
	if err != nil {
		return option.Outbound{}, err
	}
	var data map[string]any
	if err := json.Unmarshal(content, &data); err != nil {
		return option.Outbound{}, err
	}
	port, err := parsePortString(stringFromMap(data, "port"))
	if err != nil {
		return option.Outbound{}, err
	}
	alterID := 0
	if value := stringFromMap(data, "aid"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			alterID = parsed
		}
	}
	query := url.Values{}
	for key, dataKey := range map[string]string{
		"type": "net",
		"host": "host",
		"path": "path",
		"sni":  "sni",
		"alpn": "alpn",
		"fp":   "fp",
	} {
		if value := stringFromMap(data, dataKey); value != "" {
			query.Set(key, value)
		}
	}
	if tlsMode := stringFromMap(data, "tls"); tlsMode != "" {
		query.Set("security", tlsMode)
	}
	if requiresUTLS(query) && !withUTLS {
		return option.Outbound{}, ErrUTLSRequired
	}

	return option.Outbound{
		Type: constant.TypeVMess,
		Tag:  tag,
		Options: &option.VMessOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     stringFromMap(data, "add"),
				ServerPort: *port,
			},
			UUID:      stringFromMap(data, "id"),
			Security:  firstNonEmpty(stringFromMap(data, "scy"), "auto"),
			AlterId:   alterID,
			Transport: buildV2RayTransport(query),
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: buildTLSOptions(query, stringFromMap(data, "add"), false),
			},
		},
	}, nil
}

func buildTLSOptions(query url.Values, serverName string, defaultEnabled bool) *option.OutboundTLSOptions {
	security := strings.ToLower(queryFirst(query, "security", "tls"))
	enabled := defaultEnabled || security == "tls" || security == "reality"
	if !enabled || security == "none" {
		return nil
	}

	tlsOptions := &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: firstNonEmpty(queryFirst(query, "sni", "servername", "server_name"), serverName),
		Insecure:   queryBool(query, "allowInsecure", "allow_insecure", "insecure"),
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
		tlsOptions.Reality = &option.OutboundRealityOptions{
			Enabled:   true,
			PublicKey: queryFirst(query, "pbk", "publicKey", "public_key"),
			ShortID:   queryFirst(query, "sid", "shortId", "short_id"),
		}
	}
	return tlsOptions
}

func requiresUTLS(query url.Values) bool {
	security := strings.ToLower(queryFirst(query, "security", "tls"))
	return security == "reality"
}

func buildV2RayTransport(query url.Values) *option.V2RayTransportOptions {
	transportType := normalizeTransportType(queryFirst(query, "type", "network"))
	switch transportType {
	case "":
		return nil
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
		return transport
	case constant.V2RayTransportTypeHTTP:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeHTTP}
		transport.HTTPOptions.Path = queryFirst(query, "path")
		if host := splitCommaList(queryFirst(query, "host")); len(host) > 0 {
			transport.HTTPOptions.Host = badoption.Listable[string](host)
		}
		return transport
	case constant.V2RayTransportTypeGRPC:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeGRPC}
		transport.GRPCOptions.ServiceName = firstNonEmpty(queryFirst(query, "serviceName", "service_name"), queryFirst(query, "path"))
		return transport
	case constant.V2RayTransportTypeHTTPUpgrade:
		transport := &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeHTTPUpgrade}
		transport.HTTPUpgradeOptions.Path = firstNonEmpty(queryFirst(query, "path"), "/")
		transport.HTTPUpgradeOptions.Host = queryFirst(query, "host")
		return transport
	case constant.V2RayTransportTypeQUIC:
		return &option.V2RayTransportOptions{Type: constant.V2RayTransportTypeQUIC}
	default:
		return nil
	}
}

func parseListenAddr(value string) (*badoption.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return nil, ErrInvalidAddress
	}
	listen := badoption.Addr(addr)
	return &listen, nil
}

func inboundUsers(username, password string) []auth.User {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return nil
	}
	return []auth.User{{Username: username, Password: password}}
}

func nodeOutboundTag(id string) string {
	return "node-" + id
}

func mappingInboundTag(id string) string {
	return "mapping-in-" + id
}

func mappingOutboundTag(id string) string {
	return "mapping-out-" + id
}

func setRuntimeInstance(instance *box.Box, status RuntimeStatus) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	singBoxRuntime.instance = instance
	singBoxRuntime.status = status
	return status
}

func setRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	singBoxRuntime.status = status
	return status
}

func setRuntimeError(err error) RuntimeStatus {
	status := RuntimeStatus{
		Running:   false,
		State:     "error",
		Error:     err.Error(),
		UpdatedAt: time.Now(),
	}
	return setRuntimeStatus(status)
}
