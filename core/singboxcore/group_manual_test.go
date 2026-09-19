package singboxcore

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

func TestForcedSelectionRetriesOnlySelectedNode(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network, func(t *testing.T) {
			failure := errors.New("selected node failed")
			selected := &selectionTestOutbound{fakeOutbound: fakeOutbound{tag: "node-b", err: failure}}
			other := &selectionTestOutbound{fakeOutbound: fakeOutbound{tag: "node-a"}}
			manager := &fakeOutboundManager{outbounds: map[string]adapter.Outbound{
				"node-a": other,
				"node-b": selected,
			}}
			group := NewDynamicGroup("manual", manager, Policy{Strategy: BalanceManual, ForceSelected: true})
			for _, id := range []string{"a", "b"} {
				if err := group.AddNode(NewNodeState(id, "node-"+id, option.Outbound{})); err != nil {
					t.Fatal(err)
				}
			}
			node, _ := group.node("b")
			node.markFailed(time.Hour, "previous blacklist", time.Now())
			if err := group.SelectNode("b"); err != nil {
				t.Fatalf("SelectNode(blacklisted) error = %v", err)
			}
			connect := func() (io.Closer, error) {
				if network == "udp" {
					return group.ListenPacket(context.Background(), M.Socksaddr{})
				}
				return group.DialContext(context.Background(), network, M.Socksaddr{})
			}
			for attempt := 0; attempt < 4; attempt++ {
				conn, err := connect()
				if err == nil {
					_ = conn.Close()
				}
				if !errors.Is(err, failure) {
					t.Fatalf("attempt %d error = %v, want selected node failure", attempt, err)
				}
				if got := group.Snapshot().Selected; got != "b" {
					t.Fatalf("selected = %q, want b", got)
				}
				if node.Snapshot(time.Now()).Blacklisted {
					t.Fatal("forced node was automatically blacklisted")
				}
			}
			selected.err = nil
			conn, err := connect()
			if err != nil {
				t.Fatalf("connect after recovery error = %v", err)
			}
			_ = conn.Close()
			if selected.calls != 5 || other.calls != 0 {
				t.Fatalf("connection attempts: selected=%d other=%d, want 5 and 0", selected.calls, other.calls)
			}
		})
	}
}

func TestForcedSelectionTrafficFailureKeepsSiblingConnections(t *testing.T) {
	var records []TrafficFailureRecord
	group := NewDynamicGroup("manual", nil, Policy{
		Strategy:      BalanceManual,
		ForceSelected: true,
		SlowThreshold: 1,
		TrafficFailureCallback: func(record TrafficFailureRecord) {
			records = append(records, record)
		},
	})
	node := NewNodeState("a", "node-a", option.Outbound{})
	if err := group.AddNode(node); err != nil {
		t.Fatal(err)
	}
	rawSibling := &scriptedConn{}
	sibling := registerTestConn(t, node, &trackedConn{Conn: rawSibling, group: group, node: node})
	defer sibling.Close()
	for i := 0; i < 4; i++ {
		conn := registerTestConn(t, node, &trackedConn{Conn: &scriptedConn{}, group: group, node: node})
		if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("Read() error = %v, want EOF", err)
		}
		_ = conn.Close()
	}
	if len(records) != 4 || node.Snapshot(time.Now()).Blacklisted || rawSibling.closed != 0 {
		t.Fatalf("records=%d node=%+v sibling closed=%d", len(records), node.Snapshot(time.Now()), rawSibling.closed)
	}
	if got := candidateIDs(group); !sameStrings(got, []string{"a"}) {
		t.Fatalf("candidates = %v, want a", got)
	}
}

func TestForcedSelectionRespectsNodeLifecycle(t *testing.T) {
	for _, state := range []string{"disabled", "removed"} {
		t.Run(state, func(t *testing.T) {
			group := NewDynamicGroup("manual", nil, Policy{Strategy: BalanceManual, ForceSelected: true})
			for _, id := range []string{"a", "b"} {
				if err := group.AddNode(NewNodeState(id, "node-"+id, option.Outbound{})); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if state == "disabled" {
				err = group.DisableNode("a")
			} else {
				err = group.RemoveNode("a", time.Hour)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := candidateIDs(group); len(got) != 0 {
				t.Fatalf("candidates after %s = %v, want none", state, got)
			}
		})
	}
}

type selectionTestOutbound struct {
	fakeOutbound
	calls int
}

func (o *selectionTestOutbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	o.calls++
	return o.fakeOutbound.DialContext(ctx, network, destination)
}

func (o *selectionTestOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	o.calls++
	if o.err != nil {
		return nil, o.err
	}
	return &selectionTestPacketConn{}, nil
}

type selectionTestPacketConn struct{ net.PacketConn }

func (*selectionTestPacketConn) Close() error { return nil }
