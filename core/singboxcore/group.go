package singboxcore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const DynamicOutboundType = "proxyhub-dynamic-group"

type Policy struct {
	Strategy            BalanceStrategy
	FailureBlacklistTTL time.Duration
	RemoveTTL           time.Duration
}

func (p Policy) normalized() Policy {
	if p.Strategy == "" {
		p.Strategy = BalanceManual
	}
	if p.FailureBlacklistTTL <= 0 {
		p.FailureBlacklistTTL = 30 * time.Second
	}
	if p.RemoveTTL <= 0 {
		p.RemoveTTL = 2 * time.Minute
	}
	return p
}

type dynamicGroupOptions struct {
	Group *DynamicGroup
}

type DynamicGroup struct {
	outbound.Adapter

	manager  adapter.OutboundManager
	policy   Policy
	balancer Balancer

	mu       sync.RWMutex
	nodes    map[string]*NodeState
	order    []string
	selected string

	removeTags func([]string) error
}

func NewDynamicGroup(tag string, manager adapter.OutboundManager, policy Policy) *DynamicGroup {
	policy = policy.normalized()
	return &DynamicGroup{
		Adapter:  outbound.NewAdapter(DynamicOutboundType, tag, []string{N.NetworkTCP, N.NetworkUDP}, nil),
		manager:  manager,
		policy:   policy,
		balancer: NewBalancer(policy.Strategy),
		nodes:    map[string]*NodeState{},
	}
}

func (g *DynamicGroup) AddNode(node *NodeState) error {
	if g == nil || node == nil || strings.TrimSpace(node.ID) == "" || strings.TrimSpace(node.Tag) == "" {
		return ErrNodeNotFound
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if existing := g.nodes[node.ID]; existing != nil {
		existing.Option = node.Option
		existing.Tag = node.Tag
		existing.Tags = append([]string(nil), node.Tags...)
		existing.enable()
		return nil
	}
	g.nodes[node.ID] = node
	g.order = append(g.order, node.ID)
	if g.selected == "" {
		g.selected = node.ID
	}
	return nil
}

func (g *DynamicGroup) UpdatePolicy(policy Policy) {
	if g == nil {
		return
	}
	policy = policy.normalized()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policy = policy
	g.balancer = NewBalancer(policy.Strategy)
}

func (g *DynamicGroup) DisableNode(nodeID string) error {
	node, err := g.node(nodeID)
	if err != nil {
		return err
	}
	node.disable()
	g.ensureSelected()
	return nil
}

func (g *DynamicGroup) RemoveNode(nodeID string, ttl time.Duration) error {
	node, err := g.node(nodeID)
	if err != nil {
		return err
	}
	node.markTombstone(ttl, time.Now())
	g.ensureSelected()
	return g.GC()
}

func (g *DynamicGroup) SelectNode(nodeID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	node := g.nodes[strings.TrimSpace(nodeID)]
	if node == nil {
		return ErrNodeNotFound
	}
	if !node.Eligible(time.Now()) {
		return fmt.Errorf("%w: %s", ErrNoAvailableNode, nodeID)
	}
	g.selected = node.ID
	return nil
}

func (g *DynamicGroup) MarkNodeAlive(nodeID string) error {
	node, err := g.node(nodeID)
	if err != nil {
		return err
	}
	node.markAlive()
	return nil
}

func (g *DynamicGroup) MarkNodeFailed(nodeID string, ttl time.Duration, reason string) error {
	node, err := g.node(nodeID)
	if err != nil {
		return err
	}
	node.markFailed(ttl, reason, time.Now())
	g.ensureSelected()
	return nil
}

func (g *DynamicGroup) policySnapshot() Policy {
	if g == nil {
		return Policy{}.normalized()
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.policy
}

func (g *DynamicGroup) GC() error {
	if g == nil {
		return nil
	}
	now := time.Now()
	type removed struct {
		id   string
		tags []string
	}
	ready := make([]removed, 0)

	g.mu.Lock()
	for id, node := range g.nodes {
		if !node.removeReady(now) {
			continue
		}
		tags := append([]string(nil), node.Tags...)
		if len(tags) == 0 {
			tags = []string{node.Tag}
		}
		ready = append(ready, removed{id: id, tags: tags})
		delete(g.nodes, id)
		g.order = removeString(g.order, id)
		if g.selected == id {
			g.selected = ""
		}
	}
	g.mu.Unlock()

	var joined error
	for _, item := range ready {
		if g.removeTags != nil {
			joined = errors.Join(joined, g.removeTags(item.tags))
			continue
		}
		for i := len(item.tags) - 1; i >= 0; i-- {
			if g.manager == nil {
				continue
			}
			if err := g.manager.Remove(item.tags[i]); err != nil {
				joined = errors.Join(joined, err)
			}
		}
	}
	g.ensureSelected()
	return joined
}

func (g *DynamicGroup) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	candidates := g.candidates()
	if len(candidates) == 0 {
		return nil, ErrNoAvailableNode
	}
	policy := g.policySnapshot()

	var joined error
	for _, node := range candidates {
		outbound, ok := g.manager.Outbound(node.Tag)
		if !ok {
			joined = errors.Join(joined, fmt.Errorf("outbound %q not found", node.Tag))
			_ = g.MarkNodeFailed(node.ID, policy.FailureBlacklistTTL, "outbound not found")
			continue
		}
		started := time.Now()
		conn, err := outbound.DialContext(ctx, network, destination)
		if err != nil {
			joined = errors.Join(joined, err)
			_ = g.MarkNodeFailed(node.ID, policy.FailureBlacklistTTL, err.Error())
			continue
		}
		node.SetLatency(time.Since(started))
		node.markAlive()
		node.incActive()
		return &trackedConn{Conn: conn, node: node}, nil
	}
	if joined != nil {
		return nil, joined
	}
	return nil, ErrNoAvailableNode
}

func (g *DynamicGroup) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	candidates := g.candidates()
	if len(candidates) == 0 {
		return nil, ErrNoAvailableNode
	}
	policy := g.policySnapshot()

	var joined error
	for _, node := range candidates {
		outbound, ok := g.manager.Outbound(node.Tag)
		if !ok {
			joined = errors.Join(joined, fmt.Errorf("outbound %q not found", node.Tag))
			_ = g.MarkNodeFailed(node.ID, policy.FailureBlacklistTTL, "outbound not found")
			continue
		}
		packetConn, err := outbound.ListenPacket(ctx, destination)
		if err != nil {
			joined = errors.Join(joined, err)
			_ = g.MarkNodeFailed(node.ID, policy.FailureBlacklistTTL, err.Error())
			continue
		}
		node.markAlive()
		node.incActive()
		return &trackedPacketConn{PacketConn: packetConn, node: node}, nil
	}
	if joined != nil {
		return nil, joined
	}
	return nil, ErrNoAvailableNode
}

func (g *DynamicGroup) Snapshot() GroupSnapshot {
	if g == nil {
		return GroupSnapshot{}
	}
	now := time.Now()
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]NodeSnapshot, 0, len(g.order))
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil {
			nodes = append(nodes, node.Snapshot(now))
		}
	}
	return GroupSnapshot{
		Tag:      g.Tag(),
		Policy:   g.policy,
		Selected: g.selected,
		Nodes:    nodes,
	}
}

func (g *DynamicGroup) node(nodeID string) (*NodeState, error) {
	if g == nil {
		return nil, ErrGroupNotFound
	}
	nodeID = strings.TrimSpace(nodeID)
	g.mu.RLock()
	node := g.nodes[nodeID]
	g.mu.RUnlock()
	if node == nil {
		return nil, ErrNodeNotFound
	}
	return node, nil
}

func (g *DynamicGroup) candidates() []*NodeState {
	if g == nil {
		return nil
	}
	now := time.Now()
	g.mu.RLock()
	strategy := g.policy.Strategy
	selected := g.nodes[g.selected]
	nodes := make([]*NodeState, 0, len(g.order))
	for _, id := range g.order {
		node := g.nodes[id]
		if node != nil && node.Eligible(now) {
			nodes = append(nodes, node)
		}
	}
	g.mu.RUnlock()

	if len(nodes) == 0 {
		return nil
	}
	if strategy == BalanceManual {
		ordered := make([]*NodeState, 0, len(nodes))
		if selected != nil && selected.Eligible(now) {
			ordered = append(ordered, selected)
		}
		for _, node := range nodes {
			if selected != nil && node.ID == selected.ID {
				continue
			}
			ordered = append(ordered, node)
		}
		return ordered
	}
	return g.balancer.Order(nodes)
}

func (g *DynamicGroup) referencedTags() map[string]struct{} {
	result := map[string]struct{}{}
	if g == nil {
		return result
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, node := range g.nodes {
		if node == nil {
			continue
		}
		tags := node.Tags
		if len(tags) == 0 {
			tags = []string{node.Tag}
		}
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				result[tag] = struct{}{}
			}
		}
	}
	return result
}

func (g *DynamicGroup) ensureSelected() {
	if g == nil {
		return
	}
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if node := g.nodes[g.selected]; node != nil && node.Eligible(now) {
		return
	}
	g.selected = ""
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil && node.Eligible(now) {
			g.selected = id
			return
		}
	}
}

func removeString(values []string, target string) []string {
	next := values[:0]
	for _, value := range values {
		if value != target {
			next = append(next, value)
		}
	}
	return next
}

type trackedConn struct {
	net.Conn
	node *NodeState
	once sync.Once
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.node.decActive()
	})
	return err
}

type trackedPacketConn struct {
	net.PacketConn
	node *NodeState
	once sync.Once
}

func (c *trackedPacketConn) Close() error {
	err := c.PacketConn.Close()
	c.once.Do(func() {
		c.node.decActive()
	})
	return err
}

type GroupSnapshot struct {
	Tag      string         `json:"tag"`
	Policy   Policy         `json:"policy"`
	Selected string         `json:"selected"`
	Nodes    []NodeSnapshot `json:"nodes"`
}

func registerDynamicOutbound(registry *outbound.Registry) {
	outbound.Register[dynamicGroupOptions](registry, DynamicOutboundType, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options dynamicGroupOptions) (adapter.Outbound, error) {
		_ = ctx
		_ = router
		_ = logger
		if options.Group == nil {
			return nil, ErrGroupNotFound
		}
		return options.Group, nil
	})
}

func sortSnapshots(groups []GroupSnapshot) {
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Tag < groups[j].Tag
	})
}
