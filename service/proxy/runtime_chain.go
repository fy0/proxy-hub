package proxy

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"

	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/service/proxyuri"
)

var nodeChainGroupTerminalNodeTagPattern = regexp.MustCompile(`^node-chain-(.+)-(\d{2})-node-(\d{2})-final-.+$`)

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

func buildNodeRuntimeOutbounds(
	ctx context.Context,
	tx model.DBTx,
	node *tables.ProxyNodeTable,
	outboundTags map[string]struct{},
	nodeCache map[string]*tables.ProxyNodeTable,
	outboundNodeCache map[string]*tables.ProxyNodeTable,
	nodeOutboundCache map[string]string,
) (string, []option.Outbound, error) {
	if node == nil {
		return constant.TypeBlock, nil, nil
	}
	if tag, ok := nodeOutboundCache[node.ID]; ok {
		return tag, nil, nil
	}
	nodeCache[node.ID] = node

	if normalizeProtocol(node.Protocol) != ProtocolChain {
		tag := nodeOutboundTag(node.ID)
		nodeOutboundCache[node.ID] = tag
		if _, exists := outboundTags[tag]; exists {
			return tag, nil, nil
		}
		outbound, err := buildNodeOutbound(node, tag)
		if err != nil {
			return "", nil, err
		}
		outboundNodeCache[tag] = node
		outboundTags[tag] = struct{}{}
		return tag, []option.Outbound{outbound}, nil
	}

	members := chainMembersForNode(node)
	if len(members) < 2 {
		return "", nil, ErrInvalidChain
	}
	chainTag, outbounds, err := buildChainRuntimeOutbounds(
		ctx,
		tx,
		node.ID,
		members,
		outboundTags,
		outboundNodeCache,
		map[string]bool{},
	)
	if err != nil {
		return "", nil, err
	}
	tag := nodeOutboundTag(node.ID)
	nodeOutboundCache[node.ID] = tag
	if _, exists := outboundTags[tag]; !exists {
		finalOutbound := chainTag.aliasOutbound(tag)
		if finalOutbound.Tag == "" {
			return "", nil, ErrInvalidChain
		}
		outbounds = append(outbounds, finalOutbound)
		outboundNodeCache[tag] = node
		outboundTags[tag] = struct{}{}
	}
	return tag, outbounds, nil
}

type chainBuildResult struct {
	tag              string
	finalOutbound    option.Outbound
	hasFinalOutbound bool
	representNode    *tables.ProxyNodeTable
}

func (r chainBuildResult) aliasOutbound(tag string) option.Outbound {
	tag = strings.TrimSpace(tag)
	if tag == "" || r.tag == "" {
		return option.Outbound{}
	}
	if r.hasFinalOutbound {
		outbound := r.finalOutbound
		outbound.Tag = tag
		return outbound
	}
	return option.Outbound{
		Type: constant.TypeSelector,
		Tag:  tag,
		Options: &option.SelectorOutboundOptions{
			Outbounds: []string{r.tag},
			Default:   r.tag,
		},
	}
}

func buildChainRuntimeOutbounds(
	ctx context.Context,
	tx model.DBTx,
	chainID string,
	members []ChainMemberDTO,
	outboundTags map[string]struct{},
	outboundNodeCache map[string]*tables.ProxyNodeTable,
	visitingGroups map[string]bool,
) (chainBuildResult, []option.Outbound, error) {
	members = normalizeChainMembers(members)
	if len(members) < 2 {
		return chainBuildResult{}, nil, ErrInvalidChain
	}

	outbounds := make([]option.Outbound, 0, len(members)+1)
	detourTag := ""
	var last chainBuildResult
	for index, member := range members {
		if index == len(members)-2 &&
			normalizeChainMemberType(member.Type) == ChainMemberTypeGroup &&
			normalizeChainMemberType(members[index+1].Type) == ChainMemberTypeNode {
			result, memberOutbounds, handled, err := buildTerminalChainGroupRuntimeOutbounds(
				ctx,
				tx,
				chainID,
				index,
				member,
				members[index+1],
				detourTag,
				outboundTags,
				outboundNodeCache,
			)
			if err != nil {
				return chainBuildResult{}, nil, err
			}
			if handled {
				outbounds = append(outbounds, memberOutbounds...)
				detourTag = result.tag
				last = result
				break
			}
		}
		result, memberOutbounds, err := buildChainMemberRuntimeOutbounds(
			ctx,
			tx,
			chainID,
			index,
			member,
			detourTag,
			outboundTags,
			outboundNodeCache,
			visitingGroups,
		)
		if err != nil {
			return chainBuildResult{}, nil, err
		}
		if result.tag == "" {
			return chainBuildResult{}, nil, ErrInvalidChain
		}
		outbounds = append(outbounds, memberOutbounds...)
		detourTag = result.tag
		last = result
	}
	return last, outbounds, nil
}

func buildChainMemberRuntimeOutbounds(
	ctx context.Context,
	tx model.DBTx,
	chainID string,
	index int,
	member ChainMemberDTO,
	detourTag string,
	outboundTags map[string]struct{},
	outboundNodeCache map[string]*tables.ProxyNodeTable,
	visitingGroups map[string]bool,
) (chainBuildResult, []option.Outbound, error) {
	switch normalizeChainMemberType(member.Type) {
	case ChainMemberTypeNode:
		nodes, err := findNodesByIDs(ctx, tx, []string{member.ID})
		if err != nil {
			return chainBuildResult{}, nil, err
		}
		if len(nodes) != 1 || normalizeProtocol(nodes[0].Protocol) == ProtocolChain {
			return chainBuildResult{}, nil, ErrInvalidChain
		}
		return buildChainNodeMemberRuntimeOutbound(
			nodes[0],
			nodeChainMemberOutboundTag(chainID, index, member.ID),
			detourTag,
			outboundTags,
			outboundNodeCache,
		)
	case ChainMemberTypeGroup:
		groups, err := findGroupsByIDs(ctx, tx, []string{member.ID})
		if err != nil {
			return chainBuildResult{}, nil, err
		}
		if len(groups) != 1 {
			return chainBuildResult{}, nil, ErrInvalidChain
		}
		return buildChainGroupMemberRuntimeOutbounds(
			ctx,
			tx,
			chainID,
			index,
			groups[0],
			detourTag,
			outboundTags,
			outboundNodeCache,
			visitingGroups,
		)
	default:
		return chainBuildResult{}, nil, ErrInvalidChain
	}
}

func buildChainNodeMemberRuntimeOutbound(
	node *tables.ProxyNodeTable,
	tag string,
	detourTag string,
	outboundTags map[string]struct{},
	outboundNodeCache map[string]*tables.ProxyNodeTable,
) (chainBuildResult, []option.Outbound, error) {
	tag = strings.TrimSpace(tag)
	if _, exists := outboundTags[tag]; exists {
		return chainBuildResult{tag: tag, representNode: node}, nil, nil
	}
	outbound, err := buildNodeOutbound(node, tag)
	if err != nil {
		return chainBuildResult{}, nil, err
	}
	if detourTag != "" {
		if err := setOutboundDetour(&outbound, detourTag); err != nil {
			return chainBuildResult{}, nil, err
		}
	}
	outboundNodeCache[tag] = node
	outboundTags[tag] = struct{}{}
	return chainBuildResult{
		tag:              tag,
		finalOutbound:    outbound,
		hasFinalOutbound: true,
		representNode:    node,
	}, []option.Outbound{outbound}, nil
}

func buildChainGroupMemberRuntimeOutbounds(
	ctx context.Context,
	tx model.DBTx,
	chainID string,
	index int,
	group *tables.ProxyGroupTable,
	detourTag string,
	outboundTags map[string]struct{},
	outboundNodeCache map[string]*tables.ProxyNodeTable,
	visitingGroups map[string]bool,
) (chainBuildResult, []option.Outbound, error) {
	if group == nil {
		return chainBuildResult{}, nil, ErrInvalidChain
	}
	if visitingGroups[group.ID] {
		return chainBuildResult{}, nil, fmt.Errorf("%w: cyclic group %s", ErrInvalidGroup, group.Name)
	}
	groupTag := nodeChainMemberGroupOutboundTag(chainID, index, group.ID)
	if _, exists := outboundTags[groupTag]; exists {
		return chainBuildResult{tag: groupTag}, nil, nil
	}
	visitingGroups[group.ID] = true
	defer delete(visitingGroups, group.ID)

	memberTags := make([]string, 0)
	outbounds := make([]option.Outbound, 0)

	for _, builtin := range decodeStringSlice(group.BuiltinTagsJSON) {
		switch builtin {
		case constantDirect:
			memberTags = append(memberTags, constant.TypeDirect)
		case constantReject, constantRejectDrop:
			memberTags = append(memberTags, constant.TypeBlock)
		}
	}

	nodes, err := findNodesByGroupOrIDs(ctx, tx, group.ID, decodeStringSlice(group.NodeIDsJSON))
	if err != nil {
		return chainBuildResult{}, nil, err
	}
	for memberIndex, childNode := range nodes {
		if normalizeProtocol(childNode.Protocol) == ProtocolChain {
			return chainBuildResult{}, nil, ErrInvalidChain
		}
		childTag := nodeChainGroupNodeOutboundTag(chainID, index, memberIndex, childNode.ID)
		result, childOutbounds, err := buildChainNodeMemberRuntimeOutbound(
			childNode,
			childTag,
			detourTag,
			outboundTags,
			outboundNodeCache,
		)
		if err != nil {
			return chainBuildResult{}, nil, nodeBuildError{node: childNode, err: err}
		}
		memberTags = append(memberTags, result.tag)
		outbounds = append(outbounds, childOutbounds...)
	}

	childGroups, err := findGroupsByIDs(ctx, tx, decodeStringSlice(group.GroupIDsJSON))
	if err != nil {
		return chainBuildResult{}, nil, err
	}
	for _, childGroup := range childGroups {
		result, childOutbounds, err := buildChainGroupMemberRuntimeOutbounds(
			ctx,
			tx,
			chainID,
			index,
			childGroup,
			detourTag,
			outboundTags,
			outboundNodeCache,
			visitingGroups,
		)
		if err != nil {
			return chainBuildResult{}, nil, err
		}
		if result.tag != "" {
			memberTags = append(memberTags, result.tag)
		}
		outbounds = append(outbounds, childOutbounds...)
	}

	memberTags = proxyuri.UniqueNonEmpty(memberTags)
	if len(memberTags) == 0 {
		memberTags = []string{constant.TypeBlock}
	}
	groupOutbound := buildProxyGroupOutbound(group, groupTag, memberTags)
	outbounds = append(outbounds, groupOutbound)
	outboundTags[groupTag] = struct{}{}
	return chainBuildResult{tag: groupTag}, outbounds, nil
}

func buildTerminalChainGroupRuntimeOutbounds(
	ctx context.Context,
	tx model.DBTx,
	chainID string,
	index int,
	groupMember ChainMemberDTO,
	finalMember ChainMemberDTO,
	detourTag string,
	outboundTags map[string]struct{},
	outboundNodeCache map[string]*tables.ProxyNodeTable,
) (chainBuildResult, []option.Outbound, bool, error) {
	groups, err := findGroupsByIDs(ctx, tx, []string{groupMember.ID})
	if err != nil {
		return chainBuildResult{}, nil, false, err
	}
	if len(groups) != 1 {
		return chainBuildResult{}, nil, false, ErrInvalidChain
	}
	group := groups[0]
	if group == nil {
		return chainBuildResult{}, nil, false, ErrInvalidChain
	}
	if len(decodeStringSlice(group.GroupIDsJSON)) > 0 || len(decodeStringSlice(group.BuiltinTagsJSON)) > 0 {
		return chainBuildResult{}, nil, false, nil
	}

	finalNodes, err := findNodesByIDs(ctx, tx, []string{finalMember.ID})
	if err != nil {
		return chainBuildResult{}, nil, false, err
	}
	if len(finalNodes) != 1 || normalizeProtocol(finalNodes[0].Protocol) == ProtocolChain {
		return chainBuildResult{}, nil, false, ErrInvalidChain
	}
	finalNode := finalNodes[0]

	groupTag := nodeChainMemberGroupOutboundTag(chainID, index, group.ID)
	if _, exists := outboundTags[groupTag]; exists {
		return chainBuildResult{tag: groupTag}, nil, true, nil
	}

	nodes, err := findNodesByGroupOrIDs(ctx, tx, group.ID, decodeStringSlice(group.NodeIDsJSON))
	if err != nil {
		return chainBuildResult{}, nil, false, err
	}
	if len(nodes) == 0 {
		return chainBuildResult{}, nil, false, nil
	}

	memberTags := make([]string, 0, len(nodes))
	outbounds := make([]option.Outbound, 0, len(nodes)*2+1)
	for memberIndex, childNode := range nodes {
		if normalizeProtocol(childNode.Protocol) == ProtocolChain {
			return chainBuildResult{}, nil, false, ErrInvalidChain
		}
		childTag := nodeChainGroupNodeOutboundTag(chainID, index, memberIndex, childNode.ID)
		childResult, childOutbounds, err := buildChainNodeMemberRuntimeOutbound(
			childNode,
			childTag,
			detourTag,
			outboundTags,
			outboundNodeCache,
		)
		if err != nil {
			return chainBuildResult{}, nil, false, err
		}
		outbounds = append(outbounds, childOutbounds...)

		finalTag := nodeChainGroupTerminalNodeOutboundTag(chainID, index, memberIndex, finalNode.ID)
		finalResult, finalOutbounds, err := buildChainNodeMemberRuntimeOutbound(
			finalNode,
			finalTag,
			childResult.tag,
			outboundTags,
			outboundNodeCache,
		)
		if err != nil {
			return chainBuildResult{}, nil, false, err
		}
		outbounds = append(outbounds, finalOutbounds...)
		memberTags = append(memberTags, finalResult.tag)
	}

	memberTags = proxyuri.UniqueNonEmpty(memberTags)
	if len(memberTags) == 0 {
		return chainBuildResult{}, nil, false, nil
	}
	groupOutbound := buildProxyGroupOutbound(group, groupTag, memberTags)
	outbounds = append(outbounds, groupOutbound)
	outboundTags[groupTag] = struct{}{}
	return chainBuildResult{tag: groupTag}, outbounds, true, nil
}

func setOutboundDetour(outbound *option.Outbound, detour string) error {
	if outbound == nil || strings.TrimSpace(detour) == "" {
		return nil
	}
	wrapper, ok := outbound.Options.(option.DialerOptionsWrapper)
	if !ok {
		return ErrInvalidChain
	}
	dialOptions := wrapper.TakeDialerOptions()
	dialOptions.Detour = detour
	wrapper.ReplaceDialerOptions(dialOptions)
	return nil
}

func buildProxyGroupOutbound(proxyGroup *tables.ProxyGroupTable, tag string, memberTags []string) option.Outbound {
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
	case ProtocolShadowsocks:
		if strings.TrimSpace(node.Username) == "" || strings.TrimSpace(node.Password) == "" {
			return option.Outbound{}, fmt.Errorf("%w: missing shadowsocks credentials", ErrUnsupportedURI)
		}
		return option.Outbound{
			Type: constant.TypeShadowsocks,
			Tag:  tag,
			Options: &option.ShadowsocksOutboundOptions{
				ServerOptions: serverOptions,
				Method:        node.Username,
				Password:      node.Password,
			},
		}, nil
	case ProtocolTUIC:
		tlsOptions := defaultOutboundTLSOptions(serverOptions.Server)
		return option.Outbound{
			Type: constant.TypeTUIC,
			Tag:  tag,
			Options: &option.TUICOutboundOptions{
				ServerOptions: serverOptions,
				UUID:          node.Username,
				Password:      node.Password,
				OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
					TLS: tlsOptions,
				},
			},
		}, nil
	case ProtocolSSH:
		return option.Outbound{
			Type: constant.TypeSSH,
			Tag:  tag,
			Options: &option.SSHOutboundOptions{
				ServerOptions: serverOptions,
				User:          node.Username,
				Password:      node.Password,
			},
		}, nil
	default:
		return option.Outbound{}, ErrUnsupportedProtocol
	}
}

func defaultOutboundTLSOptions(serverName string) *option.OutboundTLSOptions {
	return &option.OutboundTLSOptions{
		Enabled:    true,
		ServerName: serverName,
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

func nodeChainMemberOutboundTag(chainID string, index int, nodeID string) string {
	return fmt.Sprintf("node-chain-%s-%02d-%s", strings.TrimSpace(chainID), index, strings.TrimSpace(nodeID))
}

func nodeChainMemberGroupOutboundTag(chainID string, index int, groupID string) string {
	return fmt.Sprintf("node-chain-%s-%02d-group-%s", strings.TrimSpace(chainID), index, strings.TrimSpace(groupID))
}

func nodeChainGroupNodeOutboundTag(chainID string, groupIndex int, memberIndex int, nodeID string) string {
	return fmt.Sprintf("node-chain-%s-%02d-node-%02d-%s", strings.TrimSpace(chainID), groupIndex, memberIndex, strings.TrimSpace(nodeID))
}

func nodeChainGroupTerminalNodeOutboundTag(chainID string, groupIndex int, memberIndex int, nodeID string) string {
	return fmt.Sprintf("node-chain-%s-%02d-node-%02d-final-%s", strings.TrimSpace(chainID), groupIndex, memberIndex, strings.TrimSpace(nodeID))
}

func parseNodeChainGroupTerminalNodeTag(tag string) (string, int, int, bool) {
	match := nodeChainGroupTerminalNodeTagPattern.FindStringSubmatch(strings.TrimSpace(tag))
	if match == nil {
		return "", 0, 0, false
	}
	groupIndex, err := strconv.Atoi(match[2])
	if err != nil {
		return "", 0, 0, false
	}
	memberIndex, err := strconv.Atoi(match[3])
	if err != nil {
		return "", 0, 0, false
	}
	return match[1], groupIndex, memberIndex, true
}

func runtimeChainGroupMemberNodeID(chainID string, groupIndex int, memberIndex int) string {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return ""
	}
	ctx := context.Background()
	nodes, err := findNodesByIDs(ctx, nil, []string{chainID})
	if err != nil || len(nodes) != 1 {
		return ""
	}
	members := chainMembersForNode(nodes[0])
	if groupIndex < 0 || groupIndex >= len(members) || normalizeChainMemberType(members[groupIndex].Type) != ChainMemberTypeGroup {
		return ""
	}
	groups, err := findGroupsByIDs(ctx, nil, []string{members[groupIndex].ID})
	if err != nil || len(groups) != 1 {
		return ""
	}
	groupNodes, err := findNodesByGroupOrIDs(ctx, nil, groups[0].ID, decodeStringSlice(groups[0].NodeIDsJSON))
	if err != nil || memberIndex < 0 || memberIndex >= len(groupNodes) {
		return ""
	}
	return groupNodes[memberIndex].ID
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

func mappingProxyGroupOutboundTag(mappingID string, groupID string) string {
	return fmt.Sprintf("mapping-group-%s-group-%s", strings.TrimSpace(mappingID), strings.TrimSpace(groupID))
}
