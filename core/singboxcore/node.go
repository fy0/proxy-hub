package singboxcore

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/option"
)

type HealthState string

const (
	HealthAlive       HealthState = "alive"
	HealthDead        HealthState = "dead"
	HealthBlacklisted HealthState = "blacklisted"
)

type NodeConfig struct {
	ID           string
	Tag          string
	URI          string
	Outbound     option.Outbound
	OutboundTags []string
}

type NodeState struct {
	ID     string
	Tag    string
	Option option.Outbound
	Tags   []string

	activeCount atomic.Int64
	lastLatency atomic.Int64

	mu               sync.RWMutex
	enabled          bool
	health           HealthState
	blacklistedUntil time.Time
	blacklistReason  string
	tombstoned       bool
	tombstonedAt     time.Time
	removeAfter      time.Time
	lastError        string

	leastLatencyCandidate      bool
	leastLatencySlowCount      int
	leastLatencyFailureCount   int
	leastLatencyLastSuccessAt  time.Time
	leastLatencyLastCheckedAt  time.Time
	leastLatencyProbeStartedAt time.Time
	leastLatencyProbeRunning   bool
	leastLatencyStaleFallback  bool
	leastLatencyLastProbeError string
}

func NewNodeState(id, tag string, outbound option.Outbound) *NodeState {
	return &NodeState{
		ID:      id,
		Tag:     tag,
		Option:  outbound,
		Tags:    []string{tag},
		enabled: true,
		health:  HealthAlive,
	}
}

func (n *NodeState) ActiveCount() int64 {
	if n == nil {
		return 0
	}
	return n.activeCount.Load()
}

func (n *NodeState) LastLatency() int64 {
	if n == nil {
		return 0
	}
	return n.lastLatency.Load()
}

func (n *NodeState) LeastLatencyCandidate() bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.leastLatencyCandidate
}

func (n *NodeState) LeastLatencyFallback() bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return !n.leastLatencyCandidate && !n.leastLatencyLastSuccessAt.IsZero()
}

func (n *NodeState) SetLatency(latency time.Duration) {
	if n == nil || latency < 0 {
		return
	}
	n.lastLatency.Store(latency.Milliseconds())
}

func (n *NodeState) incActive() {
	n.activeCount.Add(1)
}

func (n *NodeState) decActive() {
	n.activeCount.Add(-1)
}

func (n *NodeState) Eligible(now time.Time) bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.enabled || n.tombstoned {
		return false
	}
	if n.health == HealthDead {
		return false
	}
	if n.health == HealthBlacklisted {
		return !n.blacklistedUntil.IsZero() && !n.blacklistedUntil.After(now)
	}
	return true
}

func (n *NodeState) Snapshot(now time.Time) NodeSnapshot {
	if n == nil {
		return NodeSnapshot{}
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	blacklisted := n.health == HealthBlacklisted && (n.blacklistedUntil.IsZero() || n.blacklistedUntil.After(now))
	return NodeSnapshot{
		ID:                n.ID,
		Tag:               n.Tag,
		Enabled:           n.enabled,
		Health:            n.health,
		Blacklisted:       blacklisted,
		BlacklistedUntil:  n.blacklistedUntil,
		BlacklistReason:   n.blacklistReason,
		Tombstoned:        n.tombstoned,
		TombstonedAt:      n.tombstonedAt,
		RemoveAfter:       n.removeAfter,
		ActiveCount:       n.activeCount.Load(),
		LastLatencyMs:     n.lastLatency.Load(),
		LastError:         n.lastError,
		LastCheckedAt:     n.leastLatencyLastCheckedAt,
		LastSuccessAt:     n.leastLatencyLastSuccessAt,
		ProbeStartedAt:    n.leastLatencyProbeStartedAt,
		ProbeRunning:      n.leastLatencyProbeRunning,
		ProbeFailureCount: n.leastLatencyFailureCount,
		LastProbeError:    n.leastLatencyLastProbeError,
		LatencyCandidate:  n.leastLatencyCandidate,
		LatencyFallback:   n.leastLatencyStaleFallback,
		LatencySlowCount:  n.leastLatencySlowCount,
	}
}

func (n *NodeState) disable() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.enabled = false
}

func (n *NodeState) enable() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.enabled = true
	if n.health == "" {
		n.health = HealthAlive
	}
}

func (n *NodeState) markAlive() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.health = HealthAlive
	n.blacklistedUntil = time.Time{}
	n.blacklistReason = ""
	n.lastError = ""
}

func (n *NodeState) markFailed(ttl time.Duration, reason string, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastError = reason
	n.leastLatencyCandidate = false
	n.leastLatencyStaleFallback = !n.leastLatencyLastSuccessAt.IsZero()
	if ttl > 0 {
		n.health = HealthBlacklisted
		n.blacklistedUntil = now.Add(ttl)
		n.blacklistReason = reason
		return
	}
	n.health = HealthDead
}

func (n *NodeState) markTombstone(ttl time.Duration, now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.enabled = false
	n.tombstoned = true
	n.tombstonedAt = now
	if ttl > 0 {
		n.removeAfter = now.Add(ttl)
	}
}

func (n *NodeState) recordLeastLatencyProbeSuccess(latency time.Duration, maxLatency time.Duration, slowThreshold int, now time.Time) {
	if n == nil {
		return
	}
	if latency < 0 {
		latency = 0
	}
	if slowThreshold <= 0 {
		slowThreshold = 1
	}
	n.lastLatency.Store(latency.Milliseconds())
	n.mu.Lock()
	defer n.mu.Unlock()
	n.leastLatencyLastCheckedAt = now
	n.leastLatencyProbeRunning = false
	n.leastLatencyProbeStartedAt = time.Time{}
	n.leastLatencyLastProbeError = ""
	n.leastLatencyFailureCount = 0
	n.health = HealthAlive
	n.blacklistedUntil = time.Time{}
	n.blacklistReason = ""
	n.lastError = ""
	n.leastLatencyLastSuccessAt = now
	if maxLatency > 0 && latency > maxLatency {
		n.leastLatencySlowCount++
		if n.leastLatencySlowCount >= slowThreshold {
			n.leastLatencyCandidate = false
			n.leastLatencyStaleFallback = true
		}
		return
	}
	n.leastLatencySlowCount = 0
	n.leastLatencyCandidate = true
	n.leastLatencyStaleFallback = false
}

type probeFailurePolicy struct {
	threshold int
	ttl       time.Duration
}

func (n *NodeState) recordLeastLatencyProbeFailure(reason string, now time.Time, policies ...probeFailurePolicy) {
	if n == nil {
		return
	}
	policy := probeFailurePolicy{}
	if len(policies) > 0 {
		policy = policies[0]
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.leastLatencyLastCheckedAt = now
	n.leastLatencyProbeRunning = false
	n.leastLatencyProbeStartedAt = time.Time{}
	n.leastLatencyLastProbeError = reason
	n.leastLatencyFailureCount++
	n.leastLatencyCandidate = false
	n.leastLatencyStaleFallback = !n.leastLatencyLastSuccessAt.IsZero()
	n.lastError = reason
	if policy.threshold > 0 && n.leastLatencyFailureCount >= policy.threshold {
		n.health = HealthBlacklisted
		if policy.ttl > 0 {
			n.blacklistedUntil = now.Add(policy.ttl)
		} else {
			n.blacklistedUntil = time.Time{}
		}
		n.blacklistReason = reason
	}
}

func (n *NodeState) markLeastLatencyProbeRunning(now time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.leastLatencyProbeRunning = true
	n.leastLatencyProbeStartedAt = now
}

func (n *NodeState) reviveBlacklist() bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.health != HealthBlacklisted {
		return false
	}
	n.health = HealthAlive
	n.blacklistedUntil = time.Time{}
	n.blacklistReason = ""
	n.lastError = ""
	n.leastLatencyLastProbeError = ""
	n.leastLatencyFailureCount = 0
	n.leastLatencySlowCount = 0
	n.leastLatencyCandidate = true
	n.leastLatencyStaleFallback = false
	return true
}

func (n *NodeState) removeReady(now time.Time) bool {
	if n == nil {
		return false
	}
	n.mu.RLock()
	tombstoned := n.tombstoned
	removeAfter := n.removeAfter
	n.mu.RUnlock()
	if !tombstoned {
		return false
	}
	if n.activeCount.Load() <= 0 {
		return true
	}
	return !removeAfter.IsZero() && !removeAfter.After(now)
}

type NodeSnapshot struct {
	ID                string      `json:"id"`
	Tag               string      `json:"tag"`
	Enabled           bool        `json:"enabled"`
	Health            HealthState `json:"health"`
	Blacklisted       bool        `json:"blacklisted"`
	BlacklistedUntil  time.Time   `json:"blacklistedUntil,omitempty"`
	BlacklistReason   string      `json:"blacklistReason,omitempty"`
	Tombstoned        bool        `json:"tombstoned"`
	TombstonedAt      time.Time   `json:"tombstonedAt,omitempty"`
	RemoveAfter       time.Time   `json:"removeAfter,omitempty"`
	ActiveCount       int64       `json:"activeCount"`
	LastLatencyMs     int64       `json:"lastLatencyMs"`
	LastError         string      `json:"lastError,omitempty"`
	LastCheckedAt     time.Time   `json:"lastCheckedAt,omitempty"`
	LastSuccessAt     time.Time   `json:"lastSuccessAt,omitempty"`
	ProbeStartedAt    time.Time   `json:"probeStartedAt,omitempty"`
	ProbeRunning      bool        `json:"probeRunning"`
	ProbeFailureCount int         `json:"probeFailureCount"`
	LastProbeError    string      `json:"lastProbeError,omitempty"`
	LatencyCandidate  bool        `json:"latencyCandidate"`
	LatencyFallback   bool        `json:"latencyFallback"`
	LatencySlowCount  int         `json:"latencySlowCount"`
}
