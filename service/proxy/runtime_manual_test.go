package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/sagernet/sing-box/constant"

	"proxy-hub/model/tables"
)

func TestDynamicRuntimePlanManualSelectionIgnoresBlacklist(t *testing.T) {
	initProxyInMemoryDB(t)
	ctx := context.Background()
	var nodes []*tables.ProxyNodeTable
	for _, name := range []string{"first", "selected"} {
		node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
			Name: name, Protocol: ProtocolSOCKS5, Server: "127.0.0.1", Port: uint16Ptr(1080),
		})
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	if _, err := NodeBlacklist(ctx, nodes[1].ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, strategy := range []string{StrategyManual, StrategyFailover, StrategyLoadBalance, StrategyLeastLatency} {
		t.Run(strategy, func(t *testing.T) {
			mapping := &tables.PortMappingTable{
				ListenAddress: "127.0.0.1", ListenPort: 18080, OutboundProtocol: OutboundProtocolMixed,
				Strategy: strategy, NodeIDsJSON: encodeStringSlice([]string{nodes[0].ID, nodes[1].ID}), ActiveNodeID: nodes[1].ID,
			}
			plan, err := buildDynamicRuntimePlanForMapping(ctx, nil, mapping, nil)
			if err != nil {
				t.Fatal(err)
			}
			group := dynamicGroupPlanByTag(plan.groups, mappingOutboundTag(mapping.ID))
			manual := strategy == StrategyManual
			if group == nil || group.policy.ForceSelected != manual {
				t.Fatalf("mapping group = %+v, want force selected=%v", group, manual)
			}
			if manual {
				if len(group.members) != 2 || group.selected != nodes[1].ID {
					t.Fatalf("manual group = %+v, want blacklisted node selected", group)
				}
			} else if len(group.members) != 1 || group.members[0].id != nodes[0].ID {
				t.Fatalf("automatic group = %+v, want only healthy node", group)
			}
			if manual {
				plan, err = buildDynamicRuntimePlanForMapping(ctx, nil, mapping, map[string]struct{}{nodes[1].ID: {}})
				if err != nil {
					t.Fatal(err)
				}
				group = dynamicGroupPlanByTag(plan.groups, mappingOutboundTag(mapping.ID))
				if group.selected != constant.TypeBlock || len(group.members) != 1 {
					t.Fatalf("excluded forced target group = %+v, want block without fallback", group)
				}
			}
		})
	}
	health, err := getNodeHealth(ctx, nil, nodes[1].ID)
	if err != nil || health == nil || !health.Blacklisted {
		t.Fatalf("health = %+v, error=%v, want global blacklist preserved", health, err)
	}
}

func TestRuntimeManualSelectionSurvivesBlacklistSyncAndReload(t *testing.T) {
	initProxyInMemoryDB(t)
	t.Cleanup(func() { _ = RuntimeStop() })
	ctx := context.Background()
	var nodes []*tables.ProxyNodeTable
	for _, name := range []string{"first", "second"} {
		node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
			Name: name, Protocol: ProtocolSOCKS5, Server: "127.0.0.1", Port: uint16Ptr(1080),
		})
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, node)
	}
	mapping, err := MappingCreate(ctx, nil, MappingUpsertRequest{
		Enabled: true, ListenAddress: "127.0.0.1", ListenPort: freeTCPPort(t), OutboundProtocol: OutboundProtocolMixed,
		Strategy: StrategyManual, NodeIDs: []string{nodes[0].ID, nodes[1].ID}, ActiveNodeID: &nodes[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if _, err := NodeBlacklist(ctx, node.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	assertSelected := func(nodeID string) {
		t.Helper()
		instance := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
		if instance == nil {
			t.Fatal("missing runtime instance")
		}
		group := snapshotGroupByTag(instance.core.Snapshot().Groups, mappingOutboundTag(mapping.ID))
		if group == nil || group.Selected != nodeID || !containsRuntimeNode(group.Nodes, nodeID) {
			t.Fatalf("group = %+v, want forced node %s", group, nodeID)
		}
	}
	assertSelected(nodes[0].ID)
	instance := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
	if err := instance.core.MarkNodeFailed(mappingOutboundTag(mapping.ID), nodes[0].ID, time.Hour, "dial failed"); err != nil {
		t.Fatal(err)
	}
	assertSelected(nodes[0].ID)
	if _, err := RuntimeSyncMapping(ctx, mapping.ID); err != nil {
		t.Fatal(err)
	}
	assertSelected(nodes[0].ID)
	if _, err := MappingSwitch(ctx, nil, mapping.ID, MappingSwitchRequest{TargetType: MappingSwitchTargetNode, TargetID: nodes[1].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeSyncMapping(ctx, mapping.ID); err != nil {
		t.Fatal(err)
	}
	assertSelected(nodes[1].ID)
	if _, err := RuntimeReload(ctx); err != nil {
		t.Fatal(err)
	}
	assertSelected(nodes[1].ID)
	for _, node := range nodes {
		health, err := getNodeHealth(ctx, nil, node.ID)
		if err != nil || health == nil || !health.Blacklisted {
			t.Fatalf("health = %+v, error=%v, want global blacklist preserved", health, err)
		}
	}
}
