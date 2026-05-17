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
		ID:               n.ID,
		Tag:              n.Tag,
		Enabled:          n.enabled,
		Health:           n.health,
		Blacklisted:      blacklisted,
		BlacklistedUntil: n.blacklistedUntil,
		BlacklistReason:  n.blacklistReason,
		Tombstoned:       n.tombstoned,
		TombstonedAt:     n.tombstonedAt,
		RemoveAfter:      n.removeAfter,
		ActiveCount:      n.activeCount.Load(),
		LastLatencyMs:    n.lastLatency.Load(),
		LastError:        n.lastError,
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
	ID               string      `json:"id"`
	Tag              string      `json:"tag"`
	Enabled          bool        `json:"enabled"`
	Health           HealthState `json:"health"`
	Blacklisted      bool        `json:"blacklisted"`
	BlacklistedUntil time.Time   `json:"blacklistedUntil,omitempty"`
	BlacklistReason  string      `json:"blacklistReason,omitempty"`
	Tombstoned       bool        `json:"tombstoned"`
	TombstonedAt     time.Time   `json:"tombstonedAt,omitempty"`
	RemoveAfter      time.Time   `json:"removeAfter,omitempty"`
	ActiveCount      int64       `json:"activeCount"`
	LastLatencyMs    int64       `json:"lastLatencyMs"`
	LastError        string      `json:"lastError,omitempty"`
}
