package singboxcore

import (
	"math/rand/v2"
	"sync/atomic"
)

type BalanceStrategy string

const (
	BalanceManual       BalanceStrategy = "manual"
	BalanceRoundRobin   BalanceStrategy = "round-robin"
	BalanceLeastLatency BalanceStrategy = "least-latency"
	BalanceRandom       BalanceStrategy = "random"
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

func (b *RandomBalancer) Order(nodes []*NodeState) []*NodeState {
	ordered := append([]*NodeState(nil), nodes...)
	rand.Shuffle(len(ordered), func(i, j int) {
		ordered[i], ordered[j] = ordered[j], ordered[i]
	})
	return ordered
}

func NewBalancer(strategy BalanceStrategy) Balancer {
	if strategy == BalanceRandom {
		return &RandomBalancer{}
	}
	return &RoundRobinBalancer{}
}
