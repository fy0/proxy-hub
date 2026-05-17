package singboxcore

import (
	"math/rand/v2"
	"sync/atomic"
)

type BalanceStrategy string

const (
	BalanceManual       BalanceStrategy = "manual"
	BalanceRoundRobin   BalanceStrategy = "round-robin"
	BalanceRandom       BalanceStrategy = "random"
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

type RandomBalancer struct{}

func (RandomBalancer) Order(nodes []*NodeState) []*NodeState {
	ordered := append([]*NodeState(nil), nodes...)
	rand.Shuffle(len(ordered), func(i, j int) {
		ordered[i], ordered[j] = ordered[j], ordered[i]
	})
	return ordered
}

type LeastLatencyBalancer struct{}

func (LeastLatencyBalancer) Order(nodes []*NodeState) []*NodeState {
	ordered := append([]*NodeState(nil), nodes...)
	for i := 1; i < len(ordered); i++ {
		current := ordered[i]
		j := i - 1
		for j >= 0 && ordered[j].LastLatency() > current.LastLatency() {
			ordered[j+1] = ordered[j]
			j--
		}
		ordered[j+1] = current
	}
	return ordered
}

func NewBalancer(strategy BalanceStrategy) Balancer {
	switch strategy {
	case BalanceRandom:
		return RandomBalancer{}
	case BalanceLeastLatency:
		return LeastLatencyBalancer{}
	default:
		return &RoundRobinBalancer{}
	}
}
