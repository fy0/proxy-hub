package singboxcore

import "sync/atomic"

type BalanceStrategy string

const (
	BalanceManual       BalanceStrategy = "manual"
	BalanceRoundRobin   BalanceStrategy = "round-robin"
	BalanceLeastLatency BalanceStrategy = "least-latency"
)

type Balancer interface {
	Order(nodes []*NodeState) []*NodeState
}

type RoundRobinBalancer struct {
	next atomic.Uint64
}

func (b *RoundRobinBalancer) Order(nodes []*NodeState) []*NodeState {
	if len(nodes) <= 1 {
		return append([]*NodeState(nil), nodes...)
	}
	start := int(b.next.Add(1)-1) % len(nodes)
	ordered := make([]*NodeState, 0, len(nodes))
	ordered = append(ordered, nodes[start:]...)
	ordered = append(ordered, nodes[:start]...)
	return ordered
}

func NewBalancer(_ BalanceStrategy) Balancer {
	return &RoundRobinBalancer{}
}
