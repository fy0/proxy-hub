package singboxcore

import (
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
)

func TestPolicyUpdatePreservesRoundRobinPosition(t *testing.T) {
	for _, strategy := range []BalanceStrategy{BalanceRoundRobin, BalanceLeastLatency} {
		t.Run(string(strategy), func(t *testing.T) {
			policy := Policy{Strategy: strategy, FallbackStrategy: BalanceRoundRobin}
			group := NewDynamicGroup("test", nil, policy)
			for _, id := range []string{"a", "b", "c"} {
				if err := group.AddNode(NewNodeState(id, id, option.Outbound{})); err != nil {
					t.Fatal(err)
				}
			}
			if got := candidateIDs(group); got[0] != "a" {
				t.Fatalf("first candidate = %v, want a", got)
			}
			policy.ProbeInterval = time.Minute
			group.UpdatePolicy(policy)
			if got := candidateIDs(group); got[0] != "b" {
				t.Fatalf("candidate after policy refresh = %v, want b", got)
			}
		})
	}
}

func TestConcurrentPolicyUpdateAndSelection(t *testing.T) {
	group := NewDynamicGroup("test", nil, Policy{Strategy: BalanceRoundRobin})
	for _, id := range []string{"a", "b"} {
		if err := group.AddNode(NewNodeState(id, id, option.Outbound{})); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			group.UpdatePolicy(Policy{Strategy: BalanceRoundRobin})
			group.UpdatePolicy(Policy{Strategy: BalanceLeastLatency, FallbackStrategy: BalanceRandom})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			if got := candidateIDs(group); len(got) != 2 {
				t.Errorf("candidates = %v, want two nodes", got)
				return
			}
		}
	}()
	wg.Wait()
}
