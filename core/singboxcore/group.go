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
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const DynamicOutboundType = "proxyhub-dynamic-group"

const (
	DefaultLeastLatencyProbeURL         = "https://www.gstatic.com/generate_204"
	DefaultLeastLatencyProbeInterval    = 3 * time.Minute
	DefaultLeastLatencyProbeConcurrency = 5
	DefaultLeastLatencyMaxLatency       = 3 * time.Second
	DefaultLeastLatencySlowThreshold    = 3
	DefaultBlacklistRevivalLimit        = 3
)

type ProbeRecord struct {
	GroupTag  string
	NodeID    string
	NodeTag   string
	Available bool
	Latency   time.Duration
	Error     string
	CheckedAt time.Time
}

type BlacklistRevivalEvent struct {
	GroupTag  string
	NodeIDs   []string
	RevivedAt time.Time
}

type ProbeResultCallback func(ProbeRecord)

type BlacklistRevivalCallback func(BlacklistRevivalEvent)

type Policy struct {
	Strategy                 BalanceStrategy
	FailureBlacklistTTL      time.Duration
	RemoveTTL                time.Duration
	ProbeURL                 string
	ProbeInterval            time.Duration
	ProbeTimeout             time.Duration
	ProbeTestTimeout         time.Duration
	ProbeConcurrency         int
	MaxLatency               time.Duration
	SlowThreshold            int
	BlacklistRevivalLimit    int
	FallbackStrategy         BalanceStrategy
	ProbeResultCallback      ProbeResultCallback      `json:"-"`
	BlacklistRevivalCallback BlacklistRevivalCallback `json:"-"`
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
	if strings.TrimSpace(p.ProbeURL) == "" {
		p.ProbeURL = DefaultLeastLatencyProbeURL
	}
	if p.ProbeInterval <= 0 {
		p.ProbeInterval = DefaultLeastLatencyProbeInterval
	}
	if p.ProbeTimeout <= 0 {
		p.ProbeTimeout = DefaultLeastLatencyMaxLatency
	}
	if p.ProbeTestTimeout <= 0 {
		p.ProbeTestTimeout = p.ProbeTimeout
	}
	if p.ProbeConcurrency <= 0 {
		p.ProbeConcurrency = DefaultLeastLatencyProbeConcurrency
	}
	if p.MaxLatency <= 0 {
		p.MaxLatency = DefaultLeastLatencyMaxLatency
	}
	if p.SlowThreshold <= 0 {
		p.SlowThreshold = DefaultLeastLatencySlowThreshold
	}
	if p.BlacklistRevivalLimit <= 0 {
		p.BlacklistRevivalLimit = DefaultBlacklistRevivalLimit
	}
	if p.FallbackStrategy == "" {
		p.FallbackStrategy = BalanceRoundRobin
	}
	return p
}

type dynamicGroupOptions struct {
	Group *DynamicGroup
}

type DynamicGroup struct {
	outbound.Adapter

	ctx      context.Context
	manager  adapter.OutboundManager
	policy   Policy
	balancer Balancer
	fallback Balancer

	mu       sync.RWMutex
	nodes    map[string]*NodeState
	order    []string
	selected string

	removeTags func([]string) error

	probeMu           sync.Mutex
	probeWake         chan struct{}
	probeRunning      bool
	probeRoundRunning bool
	runtimeStarted    bool
	lastProbeAt       time.Time
	nextProbeAt       time.Time
}

func NewDynamicGroup(tag string, manager adapter.OutboundManager, policy Policy, contexts ...context.Context) *DynamicGroup {
	policy = policy.normalized()
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	return &DynamicGroup{
		Adapter:   outbound.NewAdapter(DynamicOutboundType, tag, []string{N.NetworkTCP, N.NetworkUDP}, nil),
		ctx:       ctx,
		manager:   manager,
		policy:    policy,
		balancer:  NewBalancer(policy.Strategy),
		fallback:  NewBalancer(policy.FallbackStrategy),
		nodes:     map[string]*NodeState{},
		probeWake: make(chan struct{}, 1),
	}
}

func (g *DynamicGroup) AddNode(node *NodeState) error {
	if g == nil || node == nil || strings.TrimSpace(node.ID) == "" || strings.TrimSpace(node.Tag) == "" {
		return ErrNodeNotFound
	}
	g.mu.Lock()
	if existing := g.nodes[node.ID]; existing != nil {
		existing.Option = node.Option
		existing.Tag = node.Tag
		existing.Tags = append([]string(nil), node.Tags...)
		existing.enable()
		g.mu.Unlock()
		g.ensureLeastLatencyProbeLoop()
		g.wakeLeastLatencyProbe()
		return nil
	}
	g.nodes[node.ID] = node
	g.order = append(g.order, node.ID)
	if g.selected == "" {
		g.selected = node.ID
	}
	g.mu.Unlock()
	g.ensureLeastLatencyProbeLoop()
	g.wakeLeastLatencyProbe()
	return nil
}

func (g *DynamicGroup) UpdatePolicy(policy Policy) {
	if g == nil {
		return
	}
	policy = policy.normalized()
	g.mu.Lock()
	g.policy = policy
	g.balancer = NewBalancer(policy.Strategy)
	g.fallback = NewBalancer(policy.FallbackStrategy)
	g.mu.Unlock()
	g.ensureLeastLatencyProbeLoop()
	g.wakeLeastLatencyProbe()
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

func (g *DynamicGroup) StartProbing() {
	if g == nil {
		return
	}
	g.probeMu.Lock()
	g.runtimeStarted = true
	g.probeMu.Unlock()
	g.ensureLeastLatencyProbeLoop()
	g.wakeLeastLatencyProbe()
}

func (g *DynamicGroup) StopProbing() {
	if g == nil {
		return
	}
	g.probeMu.Lock()
	g.runtimeStarted = false
	g.probeMu.Unlock()
	g.wakeLeastLatencyProbe()
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
		if policy.Strategy == BalanceLeastLatency {
			g.setSelected(node.ID)
		}
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
		if policy.Strategy == BalanceLeastLatency {
			g.setSelected(node.ID)
		}
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
	tag := g.Tag()
	policy := g.policy
	selected := g.selected
	g.mu.RUnlock()

	g.probeMu.Lock()
	probeRunning := g.probeRoundRunning
	runtimeStarted := g.runtimeStarted
	lastProbeAt := g.lastProbeAt
	nextProbeAt := g.nextProbeAt
	g.probeMu.Unlock()

	g.mu.RLock()
	nodes := make([]NodeSnapshot, 0, len(g.order))
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil {
			nodes = append(nodes, node.Snapshot(now))
		}
	}
	g.mu.RUnlock()
	return GroupSnapshot{
		Tag:            tag,
		Policy:         policy,
		Selected:       selected,
		ProbeRunning:   probeRunning,
		RuntimeStarted: runtimeStarted,
		LastProbeAt:    lastProbeAt,
		NextProbeAt:    nextProbeAt,
		Nodes:          nodes,
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
	strategy, selected, nodes := g.candidateState(now)
	if len(nodes) == 0 && g.reviveBlacklistedNodes(now) {
		strategy, selected, nodes = g.candidateState(now)
	}

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
	if strategy == BalanceLeastLatency {
		policy := g.policySnapshot()
		return g.orderLeastLatencyCandidates(nodes, policy.FallbackStrategy)
	}
	return g.balancer.Order(nodes)
}

func (g *DynamicGroup) candidateState(now time.Time) (BalanceStrategy, *NodeState, []*NodeState) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	strategy := g.policy.Strategy
	selected := g.nodes[g.selected]
	nodes := make([]*NodeState, 0, len(g.order))
	for _, id := range g.order {
		node := g.nodes[id]
		if node != nil && node.Eligible(now) {
			nodes = append(nodes, node)
		}
	}
	return strategy, selected, nodes
}

func (g *DynamicGroup) reviveBlacklistedNodes(now time.Time) bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	nodes := make([]*NodeState, 0, len(g.order))
	for _, id := range g.order {
		if node := g.nodes[id]; node != nil {
			nodes = append(nodes, node)
		}
	}
	policy := g.policy
	groupTag := g.Tag()
	g.mu.RUnlock()

	candidates := make([]blacklistRevivalCandidate, 0, len(nodes))
	for order, node := range nodes {
		if node.Eligible(now) {
			continue
		}
		candidate, ok := node.blacklistRevivalCandidate(now, order)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return false
	}
	sortBlacklistRevivalCandidates(candidates)
	limit := policy.BlacklistRevivalLimit
	if limit <= 0 {
		limit = DefaultBlacklistRevivalLimit
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}

	revivedIDs := make([]string, 0)
	for i := 0; i < limit; i++ {
		if candidates[i].node.reviveBlacklist() {
			revivedIDs = append(revivedIDs, candidates[i].node.ID)
		}
	}
	if len(revivedIDs) == 0 {
		return false
	}
	if policy.BlacklistRevivalCallback != nil {
		policy.BlacklistRevivalCallback(BlacklistRevivalEvent{
			GroupTag:  groupTag,
			NodeIDs:   append([]string(nil), revivedIDs...),
			RevivedAt: now,
		})
	}
	return true
}

func sortBlacklistRevivalCandidates(candidates []blacklistRevivalCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return blacklistRevivalCandidateLess(candidates[i], candidates[j])
	})
}

func blacklistRevivalCandidateLess(left, right blacklistRevivalCandidate) bool {
	if left.failureCount != right.failureCount {
		return left.failureCount < right.failureCount
	}
	if left.hasSuccess != right.hasSuccess {
		return left.hasSuccess
	}
	if !left.lastSuccessAt.Equal(right.lastSuccessAt) {
		return left.lastSuccessAt.After(right.lastSuccessAt)
	}
	if left.hasLatency != right.hasLatency {
		return left.hasLatency
	}
	if left.latencyMs != right.latencyMs {
		return left.latencyMs < right.latencyMs
	}
	if !left.lastCheckedAt.Equal(right.lastCheckedAt) {
		if left.lastCheckedAt.IsZero() {
			return true
		}
		if right.lastCheckedAt.IsZero() {
			return false
		}
		return left.lastCheckedAt.Before(right.lastCheckedAt)
	}
	if !left.blacklistedAt.Equal(right.blacklistedAt) {
		if left.blacklistedAt.IsZero() {
			return false
		}
		if right.blacklistedAt.IsZero() {
			return true
		}
		return left.blacklistedAt.Before(right.blacklistedAt)
	}
	if !left.blacklistedUntil.Equal(right.blacklistedUntil) {
		if left.blacklistedUntil.IsZero() {
			return false
		}
		if right.blacklistedUntil.IsZero() {
			return true
		}
		return left.blacklistedUntil.Before(right.blacklistedUntil)
	}
	return left.order < right.order
}

func (g *DynamicGroup) orderLeastLatencyCandidates(nodes []*NodeState, fallbackStrategy BalanceStrategy) []*NodeState {
	candidates := make([]*NodeState, 0, len(nodes))
	for _, node := range nodes {
		if node.LeastLatencyCandidate() {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		for _, node := range nodes {
			if node.LeastLatencyFallback() {
				candidates = append(candidates, node)
			}
		}
	}
	if len(candidates) == 0 {
		if fallbackStrategy == BalanceManual {
			return append([]*NodeState(nil), nodes...)
		}
		if g.fallback != nil {
			return g.fallback.Order(nodes)
		}
		return NewBalancer(fallbackStrategy).Order(nodes)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return latencySortValue(candidates[i]) < latencySortValue(candidates[j])
	})
	return candidates
}

func latencySortValue(node *NodeState) int64 {
	if node == nil {
		return 1<<63 - 1
	}
	latency := node.LastLatency()
	if latency <= 0 {
		return 1<<63 - 1
	}
	return latency
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

func (g *DynamicGroup) setSelected(nodeID string) {
	if g == nil || strings.TrimSpace(nodeID) == "" {
		return
	}
	g.mu.Lock()
	if g.nodes[nodeID] != nil {
		g.selected = nodeID
	}
	g.mu.Unlock()
}

func (g *DynamicGroup) RunLeastLatencyProbeRound() {
	if g == nil {
		return
	}
	policy := g.policySnapshot()
	if policy.Strategy != BalanceLeastLatency {
		return
	}
	if policy.ProbeTestTimeout > 0 {
		policy.ProbeTimeout = policy.ProbeTestTimeout
	}
	g.runLeastLatencyProbeRound(policy)
}

func (g *DynamicGroup) SelectBestLeastLatencyCandidate() {
	if g == nil {
		return
	}
	policy := g.policySnapshot()
	if policy.Strategy != BalanceLeastLatency {
		return
	}
	candidates := g.candidates()
	if len(candidates) == 0 {
		return
	}
	g.setSelected(candidates[0].ID)
}

func (g *DynamicGroup) ensureLeastLatencyProbeLoop() {
	if g == nil {
		return
	}
	policy := g.policySnapshot()
	g.probeMu.Lock()
	defer g.probeMu.Unlock()
	if !g.runtimeStarted || policy.Strategy != BalanceLeastLatency || g.probeRunning {
		return
	}
	g.probeRunning = true
	go g.leastLatencyProbeLoop()
}

func (g *DynamicGroup) wakeLeastLatencyProbe() {
	if g == nil {
		return
	}
	select {
	case g.probeWake <- struct{}{}:
	default:
	}
}

func (g *DynamicGroup) leastLatencyProbeLoop() {
	defer func() {
		g.probeMu.Lock()
		g.probeRunning = false
		g.probeMu.Unlock()
	}()
	for {
		policy := g.policySnapshot()
		if !g.probeLoopActive(policy) {
			return
		}
		g.drainProbeWake()
		g.runLeastLatencyProbeRound(policy)
		now := time.Now()
		g.probeMu.Lock()
		g.lastProbeAt = now
		g.nextProbeAt = now.Add(policy.ProbeInterval)
		g.probeMu.Unlock()
		timer := time.NewTimer(policy.ProbeInterval)
		select {
		case <-g.ctx.Done():
			timer.Stop()
			return
		case <-g.probeWake:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}

func (g *DynamicGroup) drainProbeWake() {
	for {
		select {
		case <-g.probeWake:
		default:
			return
		}
	}
}

func (g *DynamicGroup) probeLoopActive(policy Policy) bool {
	g.probeMu.Lock()
	defer g.probeMu.Unlock()
	return g.runtimeStarted && policy.Strategy == BalanceLeastLatency
}

func (g *DynamicGroup) runLeastLatencyProbeRound(policy Policy) {
	g.probeMu.Lock()
	g.lastProbeAt = time.Now()
	g.probeRoundRunning = true
	g.probeMu.Unlock()
	defer func() {
		g.probeMu.Lock()
		g.probeRoundRunning = false
		g.probeMu.Unlock()
	}()
	nodes := g.probeCandidates()
	if len(nodes) == 0 {
		return
	}
	workers := policy.ProbeConcurrency
	if workers > len(nodes) {
		workers = len(nodes)
	}
	jobs := make(chan *NodeState)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range jobs {
				g.probeLeastLatencyNode(policy, node)
			}
		}()
	}
	for _, node := range nodes {
		select {
		case <-g.ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- node:
		}
	}
	close(jobs)
	wg.Wait()
}

func (g *DynamicGroup) probeCandidates() []*NodeState {
	now := time.Now()
	nodes := g.eligibleNodes(now)
	if len(nodes) == 0 && g.reviveBlacklistedNodes(now) {
		nodes = g.eligibleNodes(now)
	}
	return nodes
}

func (g *DynamicGroup) eligibleNodes(now time.Time) []*NodeState {
	g.mu.RLock()
	defer g.mu.RUnlock()
	nodes := make([]*NodeState, 0, len(g.order))
	for _, id := range g.order {
		node := g.nodes[id]
		if node != nil && node.Eligible(now) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (g *DynamicGroup) probeLeastLatencyNode(policy Policy, node *NodeState) {
	if g == nil || node == nil || g.manager == nil {
		return
	}
	node.markLeastLatencyProbeRunning(time.Now())
	outbound, ok := g.manager.Outbound(node.Tag)
	if !ok {
		now := time.Now()
		node.recordLeastLatencyProbeFailure("outbound not found", now, probeFailurePolicy{threshold: policy.SlowThreshold, ttl: policy.FailureBlacklistTTL})
		g.emitProbeResult(policy, node, false, 0, "outbound not found", now)
		return
	}
	timeout := policy.ProbeTimeout
	ctx, cancel := context.WithTimeout(g.ctx, timeout)
	defer cancel()
	latency, err := urltest.URLTest(ctx, policy.ProbeURL, outbound)
	now := time.Now()
	if err != nil {
		node.recordLeastLatencyProbeFailure(err.Error(), now, probeFailurePolicy{threshold: policy.SlowThreshold, ttl: policy.FailureBlacklistTTL})
		g.emitProbeResult(policy, node, false, 0, err.Error(), now)
		return
	}
	node.recordLeastLatencyProbeSuccess(time.Duration(latency)*time.Millisecond, policy.MaxLatency, policy.SlowThreshold, now)
	g.emitProbeResult(policy, node, true, time.Duration(latency)*time.Millisecond, "", now)
}

func (g *DynamicGroup) emitProbeResult(policy Policy, node *NodeState, available bool, latency time.Duration, errMessage string, checkedAt time.Time) {
	if policy.ProbeResultCallback == nil || node == nil {
		return
	}
	policy.ProbeResultCallback(ProbeRecord{
		GroupTag:  g.Tag(),
		NodeID:    node.ID,
		NodeTag:   node.Tag,
		Available: available,
		Latency:   latency,
		Error:     errMessage,
		CheckedAt: checkedAt,
	})
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
	Tag            string         `json:"tag"`
	Policy         Policy         `json:"policy"`
	Selected       string         `json:"selected"`
	ProbeRunning   bool           `json:"probeRunning"`
	RuntimeStarted bool           `json:"runtimeStarted"`
	LastProbeAt    time.Time      `json:"lastProbeAt,omitempty"`
	NextProbeAt    time.Time      `json:"nextProbeAt,omitempty"`
	Nodes          []NodeSnapshot `json:"nodes"`
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
