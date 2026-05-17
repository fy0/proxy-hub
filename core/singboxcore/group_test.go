package singboxcore

import (
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
