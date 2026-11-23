package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model/tables"
)

// A node removed from a running group by a health blacklist must be re-added
// when a successful health record clears the blacklist.
func TestRuntimeRestoresRecoveredNode(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	mapping, nodeA, _ := createTwoNodeFailoverMapping(t, ctx)

	instance := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
	if instance == nil || instance.core == nil {
		t.Fatalf("runtime instance was not created")
	}

	if _, err := NodeBlacklist(ctx, nodeA.ID, time.Hour); err != nil {
		t.Fatalf("NodeBlacklist() error = %v", err)
	}
	group := snapshotGroupByTag(instance.core.Snapshot().Groups, mappingOutboundTag(mapping.ID))
	if group == nil {
		t.Fatalf("mapping group missing after blacklist sync")
	}
	if containsRuntimeNode(group.Nodes, nodeA.ID) {
		t.Fatalf("blacklisted node still in group: %+v", group.Nodes)
	}

	// Simulate the node recovering: a successful health record clears the
	// blacklist exactly like a passing probe would.
	if _, err := recordNodeHealthResult(ctx, nil, nodeA.ID, nodeHealthResultRecord{
		Source:    nodeHealthSourceNodeProbe,
		Available: true,
		CheckedAt: time.Now(),
	}); err != nil {
		t.Fatalf("recordNodeHealthResult() error = %v", err)
	}
	health, err := getNodeHealth(ctx, nil, nodeA.ID)
	if err != nil {
		t.Fatalf("getNodeHealth() error = %v", err)
	}
	if health == nil || health.Blacklisted {
		t.Fatalf("health = %+v, want blacklist cleared after recovery", health)
	}

	group = snapshotGroupByTag(instance.core.Snapshot().Groups, mappingOutboundTag(mapping.ID))
	if group == nil {
		t.Fatalf("mapping group missing after recovery")
	}
	if !containsRuntimeNode(group.Nodes, nodeA.ID) {
		t.Fatalf("recovered node was not restored to runtime group: %+v", group.Nodes)
	}
}

// A node whose BlacklistedUntil passes without any probe must be swept by
// expireBlacklisted and restored to running groups.
func TestRuntimeRestoresNodeAfterBlacklistExpiry(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() {
		_ = RuntimeStop()
	})

	ctx := context.Background()
	mapping, nodeA, _ := createTwoNodeFailoverMapping(t, ctx)

	instance := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
	if instance == nil || instance.core == nil {
		t.Fatalf("runtime instance was not created")
	}

	if _, err := NodeBlacklist(ctx, nodeA.ID, 50*time.Millisecond); err != nil {
		t.Fatalf("NodeBlacklist() error = %v", err)
	}
	group := snapshotGroupByTag(instance.core.Snapshot().Groups, mappingOutboundTag(mapping.ID))
	if group == nil {
		t.Fatalf("mapping group missing after blacklist sync")
	}
	if containsRuntimeNode(group.Nodes, nodeA.ID) {
		t.Fatalf("blacklisted node still in group: %+v", group.Nodes)
	}

	time.Sleep(100 * time.Millisecond)
	globalNodeHealthBatcher.expireBlacklisted(ctx, time.Now())

	health, err := getNodeHealth(ctx, nil, nodeA.ID)
	if err != nil {
		t.Fatalf("getNodeHealth() error = %v", err)
	}
	if health == nil || health.Blacklisted {
		t.Fatalf("health = %+v, want blacklist cleared after expiry", health)
	}
	group = snapshotGroupByTag(instance.core.Snapshot().Groups, mappingOutboundTag(mapping.ID))
	if group == nil {
		t.Fatalf("mapping group missing after blacklist expiry")
	}
	if !containsRuntimeNode(group.Nodes, nodeA.ID) {
		t.Fatalf("node was not restored to runtime group after blacklist expiry: %+v", group.Nodes)
	}
}

// Rotating a group plan through [block] -> [node] -> [block] must leave the
// builtin member eligible and selected again; previously disabled or
// tombstoned members are re-enabled when they re-enter the plan.
func TestSyncDynamicGroupMembersRestoresBuiltinMember(t *testing.T) {
	core, err := singboxcore.NewCore(singboxcore.Config{
		Context: context.Background(),
		Options: option.Options{
			Log:       &option.LogOptions{Disabled: true},
			Outbounds: singboxcore.BaseOutbounds(),
		},
	})
	if err != nil {
		t.Fatalf("NewCore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = core.Close()
	})

	groupTag := "test-group"
	if _, err := core.UpsertGroup(groupTag, singboxcore.Policy{Strategy: singboxcore.BalanceManual}); err != nil {
		t.Fatalf("UpsertGroup() error = %v", err)
	}
	nodeMember := dynamicMemberPlan{
		id:  "node-a",
		tag: "node-a",
		outbound: option.Outbound{
			Type:    constant.TypeDirect,
			Tag:     "node-a",
			Options: &option.DirectOutboundOptions{},
		},
	}
	syncPlan := func(members []dynamicMemberPlan, selected string) {
		t.Helper()
		if err := syncDynamicGroupMembers(core, dynamicGroupPlan{
			tag:      groupTag,
			members:  members,
			selected: selected,
		}); err != nil {
			t.Fatalf("syncDynamicGroupMembers() error = %v", err)
		}
	}
	snapshotNode := func(nodeID string) *singboxcore.NodeSnapshot {
		t.Helper()
		group := snapshotGroupByTag(core.Snapshot().Groups, groupTag)
		if group == nil {
			t.Fatalf("group %q missing", groupTag)
		}
		for index := range group.Nodes {
			if group.Nodes[index].ID == nodeID {
				return &group.Nodes[index]
			}
		}
		return nil
	}

	syncPlan([]dynamicMemberPlan{builtinMember(constant.TypeBlock)}, constant.TypeBlock)
	if block := snapshotNode(constant.TypeBlock); block == nil || !block.Enabled {
		t.Fatalf("block member = %+v, want enabled", block)
	}

	syncPlan([]dynamicMemberPlan{nodeMember}, nodeMember.id)
	block := snapshotNode(constant.TypeBlock)
	if block == nil || block.Enabled {
		t.Fatalf("block member = %+v, want disabled after leaving plan", block)
	}
	if node := snapshotNode(nodeMember.id); node == nil || !node.Enabled {
		t.Fatalf("node member = %+v, want enabled", node)
	}

	syncPlan([]dynamicMemberPlan{builtinMember(constant.TypeBlock)}, constant.TypeBlock)
	block = snapshotNode(constant.TypeBlock)
	if block == nil || !block.Enabled || block.Tombstoned {
		t.Fatalf("block member = %+v, want eligible after rejoining plan", block)
	}
	if node := snapshotNode(nodeMember.id); node != nil {
		t.Fatalf("node member = %+v, want removed", node)
	}
	group := snapshotGroupByTag(core.Snapshot().Groups, groupTag)
	if group.Selected != constant.TypeBlock {
		t.Fatalf("group selected = %q, want %q", group.Selected, constant.TypeBlock)
	}
}

func createTwoNodeFailoverMapping(t *testing.T, ctx context.Context) (*tables.PortMappingTable, *tables.ProxyNodeTable, *tables.ProxyNodeTable) {
	t.Helper()

	portA := uint16(65031)
	nodeA, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node-a",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &portA,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node-a) error = %v", err)
	}
	portB := uint16(65032)
	nodeB, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "node-b",
		Protocol: ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &portB,
	})
	if err != nil {
		t.Fatalf("NodeCreate(node-b) error = %v", err)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       freeTCPPort(t),
		OutboundProtocol: OutboundProtocolMixed,
		Strategy:         StrategyFailover,
		NodeIDs:          []string{nodeA.ID, nodeB.ID},
		ActiveNodeID:     &nodeA.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}
	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatalf("RuntimeReload() error = %v", err)
	}
	return mapping, nodeA, nodeB
}
