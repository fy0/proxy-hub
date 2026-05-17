package singboxcore

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

func TestRoundRobinSelection(t *testing.T) {
	group := NewDynamicGroup("group-auto", nil, Policy{Strategy: BalanceRoundRobin})
	for _, id := range []string{"a", "b", "c"} {
		if err := group.AddNode(NewNodeState(id, "node-"+id, option.Outbound{})); err != nil {
			t.Fatalf("AddNode(%s) error = %v", id, err)
		}
	}

	if got := candidateIDs(group); !sameStrings(got, []string{"a", "b", "c"}) {
		t.Fatalf("first order = %v, want a,b,c", got)
	}
	if got := candidateIDs(group); !sameStrings(got, []string{"b", "c", "a"}) {
		t.Fatalf("second order = %v, want b,c,a", got)
	}
	if got := candidateIDs(group); !sameStrings(got, []string{"c", "a", "b"}) {
		t.Fatalf("third order = %v, want c,a,b", got)
	}
}

func TestBlacklistTTL(t *testing.T) {
	group := NewDynamicGroup("group-auto", nil, Policy{Strategy: BalanceManual})
	if err := group.AddNode(NewNodeState("a", "node-a", option.Outbound{})); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}
	if err := group.MarkNodeFailed("a", 30*time.Millisecond, "dial failed"); err != nil {
		t.Fatalf("MarkNodeFailed() error = %v", err)
	}
	if got := candidateIDs(group); len(got) != 0 {
		t.Fatalf("blacklisted candidates = %v, want empty", got)
	}
	time.Sleep(50 * time.Millisecond)
	if got := candidateIDs(group); !sameStrings(got, []string{"a"}) {
		t.Fatalf("expired blacklist candidates = %v, want a", got)
	}
}

func TestTombstoneDelayedRemoval(t *testing.T) {
	manager := &fakeOutboundManager{removed: map[string]bool{}}
	group := NewDynamicGroup("group-auto", manager, Policy{RemoveTTL: time.Hour})
	node := NewNodeState("a", "node-a", option.Outbound{})
	if err := group.AddNode(node); err != nil {
		t.Fatalf("AddNode() error = %v", err)
	}
	node.incActive()
	if err := group.RemoveNode("a", time.Hour); err != nil {
		t.Fatalf("RemoveNode() error = %v", err)
	}
	if manager.removed["node-a"] {
		t.Fatalf("node removed while active")
	}
	node.decActive()
	if err := group.GC(); err != nil {
		t.Fatalf("GC() error = %v", err)
	}
	if !manager.removed["node-a"] {
		t.Fatalf("node was not removed after active count reached zero")
	}
}

func TestRemoveNodeKeepsSharedOutbound(t *testing.T) {
	manager := &fakeOutboundManager{removed: map[string]bool{}}
	groupA := NewDynamicGroup("group-a", manager, Policy{RemoveTTL: time.Hour})
	groupB := NewDynamicGroup("group-b", manager, Policy{RemoveTTL: time.Hour})

	sharedA := NewNodeState("shared", "node-shared", option.Outbound{})
	sharedB := NewNodeState("shared", "node-shared", option.Outbound{})
	if err := groupA.AddNode(sharedA); err != nil {
		t.Fatalf("groupA.AddNode() error = %v", err)
	}
	if err := groupB.AddNode(sharedB); err != nil {
		t.Fatalf("groupB.AddNode() error = %v", err)
	}
	groupA.removeTags = func(tags []string) error {
		referenced := groupB.referencedTags()
		for _, tag := range tags {
			if _, ok := referenced[tag]; ok {
				continue
			}
			if err := manager.Remove(tag); err != nil {
				return err
			}
		}
		return nil
	}

	if err := groupA.RemoveNode("shared", time.Hour); err != nil {
		t.Fatalf("RemoveNode() error = %v", err)
	}
	if manager.removed["node-shared"] {
		t.Fatalf("shared outbound was removed while another group still references it")
	}
}

func TestActiveConnectionCount(t *testing.T) {
	node := NewNodeState("a", "node-a", option.Outbound{})
	left, right := net.Pipe()
	defer right.Close()
	node.incActive()
	conn := &trackedConn{Conn: left, node: node}
	if got := node.ActiveCount(); got != 1 {
		t.Fatalf("active count = %d, want 1", got)
	}
	_ = conn.Close()
	_ = conn.Close()
	if got := node.ActiveCount(); got != 0 {
		t.Fatalf("active count after close = %d, want 0", got)
	}
}

func TestLeastLatencyUsesOnlyQualifiedCandidates(t *testing.T) {
	group := NewDynamicGroup("group-latency", nil, Policy{Strategy: BalanceLeastLatency})
	fast := NewNodeState("fast", "node-fast", option.Outbound{})
	slow := NewNodeState("slow", "node-slow", option.Outbound{})
	if err := group.AddNode(fast); err != nil {
		t.Fatalf("AddNode(fast) error = %v", err)
	}
	if err := group.AddNode(slow); err != nil {
		t.Fatalf("AddNode(slow) error = %v", err)
	}

	now := time.Now()
	fast.recordLeastLatencyProbeSuccess(100*time.Millisecond, 3*time.Second, 3, now)
	slow.recordLeastLatencyProbeSuccess(11*time.Second, 3*time.Second, 3, now)

	if got := candidateIDs(group); !sameStrings(got, []string{"fast"}) {
		t.Fatalf("least latency candidates = %v, want fast only", got)
	}
}

func TestLeastLatencyRequiresConsecutiveSlowProbes(t *testing.T) {
	node := NewNodeState("node", "node", option.Outbound{})
	now := time.Now()
	node.recordLeastLatencyProbeSuccess(100*time.Millisecond, 3*time.Second, 3, now)

	node.recordLeastLatencyProbeSuccess(4*time.Second, 3*time.Second, 3, now.Add(time.Second))
	if !node.LeastLatencyCandidate() {
		t.Fatalf("node removed from candidate pool after one slow probe")
	}
	node.recordLeastLatencyProbeSuccess(5*time.Second, 3*time.Second, 3, now.Add(2*time.Second))
	if !node.LeastLatencyCandidate() {
		t.Fatalf("node removed from candidate pool after two slow probes")
	}
	node.recordLeastLatencyProbeSuccess(6*time.Second, 3*time.Second, 3, now.Add(3*time.Second))
	if node.LeastLatencyCandidate() {
		t.Fatalf("node remained candidate after three consecutive slow probes")
	}
	if !node.LeastLatencyFallback() {
		t.Fatalf("node did not become stale fallback after slow removal")
	}
}

func TestLeastLatencyFallsBackToStaleSuccessfulNodes(t *testing.T) {
	group := NewDynamicGroup("group-latency", nil, Policy{Strategy: BalanceLeastLatency})
	olderSlow := NewNodeState("older-slow", "node-older-slow", option.Outbound{})
	recentFast := NewNodeState("recent-fast", "node-recent-fast", option.Outbound{})
	if err := group.AddNode(olderSlow); err != nil {
		t.Fatalf("AddNode(olderSlow) error = %v", err)
	}
	if err := group.AddNode(recentFast); err != nil {
		t.Fatalf("AddNode(recentFast) error = %v", err)
	}

	now := time.Now()
	olderSlow.recordLeastLatencyProbeSuccess(700*time.Millisecond, 3*time.Second, 3, now)
	recentFast.recordLeastLatencyProbeSuccess(120*time.Millisecond, 3*time.Second, 3, now)
	olderSlow.recordLeastLatencyProbeFailure("temporary failure", now.Add(time.Second))
	recentFast.recordLeastLatencyProbeFailure("temporary failure", now.Add(time.Second))

	if got := candidateIDs(group); !sameStrings(got, []string{"recent-fast", "older-slow"}) {
		t.Fatalf("least latency fallback candidates = %v, want recent-fast then older-slow", got)
	}
}

func TestLeastLatencyToleranceKeepsCurrentSelection(t *testing.T) {
	group := NewDynamicGroup("group-latency", nil, Policy{
		Strategy:         BalanceLeastLatency,
		LatencyTolerance: 50 * time.Millisecond,
	})
	current := NewNodeState("current", "node-current", option.Outbound{})
	best := NewNodeState("best", "node-best", option.Outbound{})
	if err := group.AddNode(current); err != nil {
		t.Fatalf("AddNode(current) error = %v", err)
	}
	if err := group.AddNode(best); err != nil {
		t.Fatalf("AddNode(best) error = %v", err)
	}
	now := time.Now()
	current.recordLeastLatencyProbeSuccess(130*time.Millisecond, 3*time.Second, 3, now)
	best.recordLeastLatencyProbeSuccess(100*time.Millisecond, 3*time.Second, 3, now)
	if err := group.SelectNode("current"); err != nil {
		t.Fatalf("SelectNode(current) error = %v", err)
	}

	if got := candidateIDs(group); !sameStrings(got, []string{"current", "best"}) {
		t.Fatalf("least latency order = %v, want current retained within tolerance", got)
	}
}

func TestLeastLatencyFallsBackBeforeProbeResults(t *testing.T) {
	group := NewDynamicGroup("group-latency", nil, Policy{
		Strategy:         BalanceLeastLatency,
		FallbackStrategy: BalanceRoundRobin,
	})
	for _, id := range []string{"a", "b", "c"} {
		if err := group.AddNode(NewNodeState(id, "node-"+id, option.Outbound{})); err != nil {
			t.Fatalf("AddNode(%s) error = %v", id, err)
		}
	}

	if got := candidateIDs(group); !sameStrings(got, []string{"a", "b", "c"}) {
		t.Fatalf("first fallback order = %v, want a,b,c", got)
	}
	if got := candidateIDs(group); !sameStrings(got, []string{"b", "c", "a"}) {
		t.Fatalf("second fallback order = %v, want b,c,a", got)
	}
}

func TestUpdatePolicyToLeastLatencyDoesNotDeadlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	group := NewDynamicGroup("group-latency", nil, Policy{Strategy: BalanceManual}, ctx)
	group.StartProbing()

	done := make(chan struct{})
	go func() {
		group.UpdatePolicy(Policy{Strategy: BalanceLeastLatency})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("UpdatePolicy deadlocked while enabling least latency")
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func candidateIDs(group *DynamicGroup) []string {
	candidates := group.candidates()
	ids := make([]string, 0, len(candidates))
	for _, node := range candidates {
		ids = append(ids, node.ID)
	}
	return ids
}

type fakeOutboundManager struct {
	adapter.OutboundManager
	removed map[string]bool
}

func (m *fakeOutboundManager) Remove(tag string) error {
	m.removed[tag] = true
	return nil
}
