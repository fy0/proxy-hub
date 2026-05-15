package proxy

import (
	"context"
	"errors"
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

type RuntimeInboundFailure struct {
	MappingID string `json:"mappingId"`
	Tag       string `json:"tag"`
	Listen    string `json:"listen"`
	Error     string `json:"error"`
}

type RuntimeStatus struct {
	Running   bool                    `json:"running"`
	State     string                  `json:"state"`
	Error     string                  `json:"error,omitempty"`
	Inbounds  []RuntimeInbound        `json:"inbounds"`
	Failures  []RuntimeInboundFailure `json:"failures"`
	UpdatedAt time.Time               `json:"updatedAt"`
}

type runtimeManager struct {
	mu        sync.Mutex
	instances map[string]*box.Box
	status    RuntimeStatus
}

var singBoxRuntime = &runtimeManager{
	instances: map[string]*box.Box{},
	status: RuntimeStatus{
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	},
}

func RuntimeStatusGet() RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	status := singBoxRuntime.status
	status.Inbounds = append([]RuntimeInbound{}, singBoxRuntime.status.Inbounds...)
	status.Failures = append([]RuntimeInboundFailure{}, singBoxRuntime.status.Failures...)
	return status
}

func RuntimeReload(ctx context.Context) (RuntimeStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	mappings, err := enabledRuntimeMappings(ctx, nil)
	if err != nil {
		status := setRuntimeError(err)
		return status, err
	}

	oldInstances := replaceRuntimeInstances(RuntimeStatus{
		Running:   false,
		State:     "reloading",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	})

	if closeErr := closeRuntimeInstances(oldInstances); closeErr != nil {
		utils.Logger.Warn("关闭旧 sing-box 实例失败", zap.Error(closeErr))
	}

	instances := make(map[string]*box.Box, len(mappings))
	inbounds := make([]RuntimeInbound, 0, len(mappings))
	failures := make([]RuntimeInboundFailure, 0)

	for _, mapping := range mappings {
		options, mappingInbounds, err := buildSingBoxOptionsFromMappings(ctx, nil, []*tables.PortMappingTable{mapping})
		if err != nil {
			failures = append(failures, runtimeFailureFromMapping(mapping, err))
			continue
		}
		if len(mappingInbounds) == 0 {
			failures = append(failures, runtimeFailureFromMapping(mapping, errors.New("runtime inbound was not created")))
			continue
		}

		inbound := mappingInbounds[0]
		instance, err := box.New(box.Options{
			Options: options,
			Context: singBoxContext(context.Background()),
		})
		if err != nil {
			failures = append(failures, runtimeFailureFromInbound(inbound, err))
			continue
		}
		if err := instance.Start(); err != nil {
			_ = instance.Close()
			failures = append(failures, runtimeFailureFromInbound(inbound, err))
			continue
		}

		instances[mapping.ID] = instance
		inbounds = append(inbounds, inbound)
	}

	return setRuntimeInstances(instances, runtimeStatusFromResults(len(mappings), inbounds, failures)), nil
}

func RuntimeStop() error {
	instances := replaceRuntimeInstances(RuntimeStatus{
		Running:   false,
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	})

	return closeRuntimeInstances(instances)
}

func BuildSingBoxOptions(ctx context.Context, tx model.DBTx) (option.Options, []RuntimeInbound, error) {
	mappings, err := enabledRuntimeMappings(ctx, tx)
	if err != nil {
		return option.Options{}, nil, err
	}
	return buildSingBoxOptionsFromMappings(ctx, tx, mappings)
}

func enabledRuntimeMappings(ctx context.Context, tx model.DBTx) ([]*tables.PortMappingTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var mappings []*tables.PortMappingTable
	if err := tx.Where("enabled = ?", true).Order(mappingOrderClause()).Find(&mappings).Error; err != nil {
		return nil, err
	}
	return mappings, nil
}

func buildSingBoxOptionsFromMappings(
	ctx context.Context,
	tx model.DBTx,
	mappings []*tables.PortMappingTable,
) (option.Options, []RuntimeInbound, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)
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
	nodeCache := map[string]*tables.ProxyNodeTable{}
	groupCache := map[string]*tables.ProxyGroupTable{}
	inbounds := make([]option.Inbound, 0, len(mappings))
	rules := make([]option.Rule, 0, len(mappings))
	statusInbounds := make([]RuntimeInbound, 0, len(mappings))

	for _, mapping := range mappings {
		nodes, err := findNodesByIDs(ctx, tx, decodeStringSlice(mapping.NodeIDsJSON))
		if err != nil {
			return option.Options{}, nil, err
		}

		memberTags := make([]string, 0, len(nodes))
		for _, node := range nodes {
			nodeCache[node.ID] = node
			tag := nodeOutboundTag(node.ID)
			memberTags = append(memberTags, tag)
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

		groups, err := findGroupsByIDs(ctx, tx, decodeStringSlice(mapping.GroupIDsJSON))
		if err != nil {
			return option.Options{}, nil, err
		}
		for _, proxyGroup := range groups {
			groupTag, groupOutbounds, err := buildProxyGroupOutbounds(
				ctx,
				tx,
				proxyGroup,
				outboundTags,
				nodeCache,
				groupCache,
				map[string]bool{},
			)
			if err != nil {
				return option.Options{}, nil, err
			}
			memberTags = append(memberTags, groupTag)
			outbounds = append(outbounds, groupOutbounds...)
		}

		routeTag, groupOutbound := buildMappingOutbound(mapping, memberTags)
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
			Listen:    mappingRuntimeListen(mapping),
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
	if mapping.ActiveGroupID != "" {
		activeTag = proxyGroupOutboundTag(mapping.ActiveGroupID)
	}
	if activeTag == "" && mapping.ActiveNodeID != "" {
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

func buildProxyGroupOutbounds(
	ctx context.Context,
	tx model.DBTx,
	proxyGroup *tables.ProxyGroupTable,
	outboundTags map[string]struct{},
	nodeCache map[string]*tables.ProxyNodeTable,
	groupCache map[string]*tables.ProxyGroupTable,
	visiting map[string]bool,
) (string, []option.Outbound, error) {
	if proxyGroup == nil {
		return constant.TypeBlock, nil, nil
	}
	tag := proxyGroupOutboundTag(proxyGroup.ID)
	if _, exists := outboundTags[tag]; exists {
		return tag, nil, nil
	}
	if visiting[proxyGroup.ID] {
		return "", nil, fmt.Errorf("%w: cyclic group %s", ErrInvalidGroup, proxyGroup.Name)
	}
	visiting[proxyGroup.ID] = true
	defer delete(visiting, proxyGroup.ID)

	memberTags := make([]string, 0)
	outbounds := make([]option.Outbound, 0)

	for _, builtin := range decodeStringSlice(proxyGroup.BuiltinTagsJSON) {
		switch builtin {
		case constantDirect:
			memberTags = append(memberTags, constant.TypeDirect)
		case constantReject, constantRejectDrop:
			memberTags = append(memberTags, constant.TypeBlock)
		}
	}

	nodes, err := findNodesByGroupOrIDs(ctx, tx, proxyGroup.ID, decodeStringSlice(proxyGroup.NodeIDsJSON))
	if err != nil {
		return "", nil, err
	}
	for _, node := range nodes {
		nodeCache[node.ID] = node
		nodeTag := nodeOutboundTag(node.ID)
		memberTags = append(memberTags, nodeTag)
		if _, exists := outboundTags[nodeTag]; exists {
			continue
		}
		outbound, err := buildNodeOutbound(node, nodeTag)
		if err != nil {
			return "", nil, fmt.Errorf("节点 %s 配置无效: %w", node.Name, err)
		}
		outbounds = append(outbounds, outbound)
		outboundTags[nodeTag] = struct{}{}
	}

	childGroups, err := findGroupsByIDs(ctx, tx, decodeStringSlice(proxyGroup.GroupIDsJSON))
	if err != nil {
		return "", nil, err
	}
	for _, childGroup := range childGroups {
		groupCache[childGroup.ID] = childGroup
		childTag, childOutbounds, err := buildProxyGroupOutbounds(ctx, tx, childGroup, outboundTags, nodeCache, groupCache, visiting)
		if err != nil {
			return "", nil, err
		}
		memberTags = append(memberTags, childTag)
		outbounds = append(outbounds, childOutbounds...)
	}

	memberTags = uniqueNonEmpty(memberTags)
	if len(memberTags) == 0 {
		memberTags = []string{constant.TypeBlock}
	}
	groupOutbound := buildProxyGroupOutbound(proxyGroup, tag, memberTags)
	outbounds = append(outbounds, groupOutbound)
	outboundTags[tag] = struct{}{}
	return tag, outbounds, nil
}

func buildProxyGroupOutbound(proxyGroup *tables.ProxyGroupTable, tag string, memberTags []string) option.Outbound {
	if normalizeGroupStrategy(proxyGroup.Strategy) == GroupStrategyURLTest {
		return option.Outbound{
			Type: constant.TypeURLTest,
			Tag:  tag,
			Options: &option.URLTestOutboundOptions{
				Outbounds:   memberTags,
				URL:         urlTestLink,
				Interval:    badoption.Duration(3 * time.Minute),
				IdleTimeout: badoption.Duration(30 * time.Minute),
			},
		}
	}
	defaultTag := memberTags[0]
	return option.Outbound{
		Type: constant.TypeSelector,
		Tag:  tag,
		Options: &option.SelectorOutboundOptions{
			Outbounds: memberTags,
			Default:   defaultTag,
		},
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
		if err != nil {
			return option.Outbound{}, err
		}
		return outbound, nil
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
	parsed, err := parseNodeURI(rawURI)
	if err != nil {
		return option.Outbound{}, err
	}
	serverOptions := option.ServerOptions{
		Server:     parsed.Server,
		ServerPort: *parsed.Port,
	}

	switch parsed.Protocol {
	case ProtocolVLESS:
		if requiresUTLS(parsed.Query) && !withUTLS {
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
				Flow:          queryFirst(parsed.Query, "flow"),
				PacketEncoding: stringPtrOrNil(
					queryFirst(parsed.Query, "packetEncoding", "packet_encoding", "packet-encoding"),
				),
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
				Transport: transport,
			},
		}, nil
	case ProtocolVMess:
		if requiresUTLS(parsed.Query) && !withUTLS {
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
		if requiresUTLS(parsed.Query) && !withUTLS {
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
				ServerOptions: serverOptions,
				Password:      parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
				Transport: transport,
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
				ServerOptions: serverOptions,
				Username:      parsed.Username,
				Password:      parsed.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
			},
		}, nil
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
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

func requiresUTLS(query url.Values) bool {
	return securityMode(query) == "reality"
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

func proxyGroupOutboundTag(id string) string {
	return "group-" + id
}

func runtimeFailureFromMapping(mapping *tables.PortMappingTable, err error) RuntimeInboundFailure {
	return RuntimeInboundFailure{
		MappingID: mapping.ID,
		Tag:       mappingInboundTag(mapping.ID),
		Listen:    mappingRuntimeListen(mapping),
		Error:     err.Error(),
	}
}

func runtimeFailureFromInbound(inbound RuntimeInbound, err error) RuntimeInboundFailure {
	return RuntimeInboundFailure{
		MappingID: inbound.MappingID,
		Tag:       inbound.Tag,
		Listen:    inbound.Listen,
		Error:     err.Error(),
	}
}

func mappingRuntimeListen(mapping *tables.PortMappingTable) string {
	if mapping == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", mapping.ListenAddress, mapping.ListenPort)
}

func runtimeStatusFromResults(
	total int,
	inbounds []RuntimeInbound,
	failures []RuntimeInboundFailure,
) RuntimeStatus {
	state := "stopped"
	errorMessage := ""
	switch {
	case total == 0:
		state = "stopped"
	case len(inbounds) > 0 && len(failures) == 0:
		state = "running"
	case len(inbounds) > 0:
		state = "degraded"
	default:
		state = "error"
		errorMessage = "all proxy runtime inbounds failed to start"
	}

	return RuntimeStatus{
		Running:   len(inbounds) > 0,
		State:     state,
		Error:     errorMessage,
		Inbounds:  append([]RuntimeInbound(nil), inbounds...),
		Failures:  append([]RuntimeInboundFailure(nil), failures...),
		UpdatedAt: time.Now(),
	}
}

func replaceRuntimeInstances(status RuntimeStatus) map[string]*box.Box {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	old := singBoxRuntime.instances
	singBoxRuntime.instances = map[string]*box.Box{}
	singBoxRuntime.status = normalizeRuntimeStatus(status)
	return old
}

func setRuntimeInstances(instances map[string]*box.Box, status RuntimeStatus) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	if instances == nil {
		instances = map[string]*box.Box{}
	}
	status = normalizeRuntimeStatus(status)
	singBoxRuntime.instances = instances
	singBoxRuntime.status = status
	return status
}

func setRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	singBoxRuntime.mu.Lock()
	defer singBoxRuntime.mu.Unlock()

	status = normalizeRuntimeStatus(status)
	singBoxRuntime.status = status
	return status
}

func normalizeRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	if status.Inbounds == nil {
		status.Inbounds = []RuntimeInbound{}
	}
	if status.Failures == nil {
		status.Failures = []RuntimeInboundFailure{}
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	return status
}

func setRuntimeError(err error) RuntimeStatus {
	status := RuntimeStatus{
		Running:   false,
		State:     "error",
		Error:     err.Error(),
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	}
	return setRuntimeStatus(status)
}

func closeRuntimeInstances(instances map[string]*box.Box) error {
	errs := make([]error, 0)
	for id, instance := range instances {
		if instance == nil {
			continue
		}
		if err := instance.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
