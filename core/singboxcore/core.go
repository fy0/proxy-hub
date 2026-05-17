package singboxcore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	adapterInbound "github.com/sagernet/sing-box/adapter/inbound"
	adapterOutbound "github.com/sagernet/sing-box/adapter/outbound"
	adapterService "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/block"
	"github.com/sagernet/sing-box/protocol/direct"
	protocolHTTP "github.com/sagernet/sing-box/protocol/http"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/ssh"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
)

type Config struct {
	Context context.Context
	Options option.Options
}

type Core struct {
	ctx    context.Context
	cancel context.CancelFunc

	instance *box.Box

	mu      sync.RWMutex
	started bool
	groups  map[string]*DynamicGroup
}

func NewCore(config Config) (*Core, error) {
	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	instance, err := box.New(box.Options{
		Context: BoxContext(ctx),
		Options: config.Options,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return &Core{
		ctx:      ctx,
		cancel:   cancel,
		instance: instance,
		groups:   map[string]*DynamicGroup{},
	}, nil
}

func (c *Core) Start() error {
	if c == nil || c.instance == nil {
		return ErrCoreNotStarted
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return nil
	}
	if err := c.instance.Start(); err != nil {
		return NormalizeStartError(err)
	}
	c.started = true
	return nil
}

func (c *Core) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.started = false
	if c.instance == nil {
		return nil
	}
	return c.instance.Close()
}

func (c *Core) Box() *box.Box {
	if c == nil {
		return nil
	}
	return c.instance
}

func (c *Core) AddGroup(groupID string, policy Policy) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return ErrGroupNotFound
	}
	if c == nil || c.instance == nil {
		return ErrCoreNotStarted
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.groups[groupID]; exists {
		return ErrGroupExists
	}
	group := NewDynamicGroup(groupID, c.instance.Outbound(), policy)
	group.removeTags = c.removeOutboundTagsIfUnused
	if err := c.createDynamicGroupOutbound(groupID, group); err != nil {
		return err
	}
	c.groups[groupID] = group
	return nil
}

func (c *Core) UpsertGroup(groupID string, policy Policy) (*DynamicGroup, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, ErrGroupNotFound
	}
	if c == nil || c.instance == nil {
		return nil, ErrCoreNotStarted
	}
	c.mu.RLock()
	group := c.groups[groupID]
	c.mu.RUnlock()
	if group != nil {
		group.UpdatePolicy(policy)
		if _, exists := c.instance.Outbound().Outbound(groupID); !exists {
			if err := c.createDynamicGroupOutbound(groupID, group); err != nil {
				return nil, err
			}
		}
		return group, nil
	}
	if err := c.AddGroup(groupID, policy); err != nil && !errors.Is(err, ErrGroupExists) {
		return nil, err
	}
	c.mu.RLock()
	group = c.groups[groupID]
	c.mu.RUnlock()
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func (c *Core) createDynamicGroupOutbound(groupID string, group *DynamicGroup) error {
	return c.instance.Outbound().Create(
		c.ctx,
		c.instance.Router(),
		c.instance.LogFactory().NewLogger("outbound/"+groupID),
		groupID,
		DynamicOutboundType,
		&dynamicGroupOptions{Group: group},
	)
}

func (c *Core) AddNode(groupID string, node NodeConfig) error {
	if strings.TrimSpace(node.URI) != "" && node.Outbound.Type == "" {
		outbound, err := OutboundFromURI(node.URI, node.Tag)
		if err != nil {
			return err
		}
		node.Outbound = outbound
	}
	return c.AddNodeOutbound(groupID, node)
}

func (c *Core) AddNodeOutbound(groupID string, node NodeConfig) error {
	group, err := c.group(groupID)
	if err != nil {
		return err
	}
	node.ID = strings.TrimSpace(node.ID)
	node.Tag = strings.TrimSpace(node.Tag)
	if node.ID == "" {
		node.ID = node.Tag
	}
	if node.Tag == "" {
		node.Tag = node.Outbound.Tag
	}
	if node.Tag == "" {
		return ErrNodeNotFound
	}
	node.Outbound.Tag = node.Tag
	if err := c.CreateOutbound(node.Outbound); err != nil {
		return err
	}
	nodeState := NewNodeState(node.ID, node.Tag, node.Outbound)
	if len(node.OutboundTags) > 0 {
		nodeState.Tags = append([]string(nil), node.OutboundTags...)
	}
	return group.AddNode(nodeState)
}

func (c *Core) CreateOutbound(outbound option.Outbound) error {
	if c == nil || c.instance == nil {
		return ErrCoreNotStarted
	}
	outbound.Tag = strings.TrimSpace(outbound.Tag)
	if outbound.Tag == "" {
		return ErrNodeNotFound
	}
	if _, exists := c.instance.Outbound().Outbound(outbound.Tag); exists {
		return nil
	}
	return c.instance.Outbound().Create(
		c.ctx,
		c.instance.Router(),
		c.instance.LogFactory().NewLogger("outbound/"+outbound.Tag),
		outbound.Tag,
		outbound.Type,
		outbound.Options,
	)
}

func (c *Core) DisableNode(groupID, nodeID string) error {
	group, err := c.group(groupID)
	if err != nil {
		return err
	}
	return group.DisableNode(nodeID)
}

func (c *Core) RemoveNode(groupID, nodeID string) error {
	group, err := c.group(groupID)
	if err != nil {
		return err
	}
	return group.RemoveNode(nodeID, group.policy.RemoveTTL)
}

func (c *Core) SelectNode(groupID, nodeID string) error {
	group, err := c.group(groupID)
	if err != nil {
		return err
	}
	return group.SelectNode(nodeID)
}

func (c *Core) MarkNodeFailed(groupID, nodeID string, ttl time.Duration, reason string) error {
	group, err := c.group(groupID)
	if err != nil {
		return err
	}
	return group.MarkNodeFailed(nodeID, ttl, reason)
}

func (c *Core) GC() error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	groups := make([]*DynamicGroup, 0, len(c.groups))
	for _, group := range c.groups {
		groups = append(groups, group)
	}
	c.mu.RUnlock()
	var joined error
	for _, group := range groups {
		joined = errors.Join(joined, group.GC())
	}
	return joined
}

func (c *Core) removeOutboundTagsIfUnused(tags []string) error {
	if c == nil || c.instance == nil {
		return nil
	}
	tags = uniqueNonEmpty(tags)
	if len(tags) == 0 {
		return nil
	}
	referenced := c.referencedOutboundTags()
	var joined error
	for i := len(tags) - 1; i >= 0; i-- {
		tag := tags[i]
		if _, ok := referenced[tag]; ok {
			continue
		}
		if _, exists := c.instance.Outbound().Outbound(tag); !exists {
			continue
		}
		joined = errors.Join(joined, c.instance.Outbound().Remove(tag))
	}
	return joined
}

func (c *Core) referencedOutboundTags() map[string]struct{} {
	result := map[string]struct{}{}
	if c == nil {
		return result
	}
	c.mu.RLock()
	groups := make([]*DynamicGroup, 0, len(c.groups))
	for _, group := range c.groups {
		groups = append(groups, group)
	}
	c.mu.RUnlock()
	for _, group := range groups {
		for tag := range group.referencedTags() {
			result[tag] = struct{}{}
		}
	}
	return result
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (c *Core) Snapshot() CoreState {
	if c == nil {
		return CoreState{}
	}
	c.mu.RLock()
	state := CoreState{
		Running: c.started,
		Groups:  make([]GroupSnapshot, 0, len(c.groups)),
	}
	for _, group := range c.groups {
		state.Groups = append(state.Groups, group.Snapshot())
	}
	c.mu.RUnlock()
	sortSnapshots(state.Groups)
	return state
}

func (c *Core) group(groupID string) (*DynamicGroup, error) {
	if c == nil {
		return nil, ErrCoreNotStarted
	}
	c.mu.RLock()
	group := c.groups[strings.TrimSpace(groupID)]
	c.mu.RUnlock()
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

type CoreState struct {
	Running bool            `json:"running"`
	Groups  []GroupSnapshot `json:"groups"`
}

func BoxContext(ctx context.Context) context.Context {
	inboundRegistry := adapterInbound.NewRegistry()
	socks.RegisterInbound(inboundRegistry)
	protocolHTTP.RegisterInbound(inboundRegistry)
	mixed.RegisterInbound(inboundRegistry)

	outboundRegistry := adapterOutbound.NewRegistry()
	registerDynamicOutbound(outboundRegistry)
	block.RegisterOutbound(outboundRegistry)
	direct.RegisterOutbound(outboundRegistry)
	socks.RegisterOutbound(outboundRegistry)
	protocolHTTP.RegisterOutbound(outboundRegistry)
	shadowsocks.RegisterOutbound(outboundRegistry)
	hysteria.RegisterOutbound(outboundRegistry)
	hysteria2.RegisterOutbound(outboundRegistry)
	vmess.RegisterOutbound(outboundRegistry)
	trojan.RegisterOutbound(outboundRegistry)
	tuic.RegisterOutbound(outboundRegistry)
	ssh.RegisterOutbound(outboundRegistry)
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

func BaseOutbounds() []option.Outbound {
	return []option.Outbound{
		{
			Type:    C.TypeDirect,
			Tag:     C.TypeDirect,
			Options: &option.DirectOutboundOptions{},
		},
		{
			Type:    C.TypeBlock,
			Tag:     C.TypeBlock,
			Options: &option.StubOptions{},
		},
	}
}

func optionOutboundBlock() option.Outbound {
	return option.Outbound{
		Type:    C.TypeBlock,
		Tag:     C.TypeBlock,
		Options: &option.StubOptions{},
	}
}
