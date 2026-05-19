package proxy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"
)

func TestRecordNodeHealthResultKeepsLatestThirty(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "edge",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 31; i++ {
		available := i%3 != 0
		errMessage := ""
		if !available {
			errMessage = fmt.Sprintf("probe failed %d", i)
		}
		if _, err := recordNodeHealthResult(ctx, nil, node.ID, nodeHealthResultRecord{
			Source:    nodeHealthSourceNodeTest,
			TargetID:  node.ID,
			ProbeURL:  "https://example.com/generate_204",
			Available: available,
			LatencyMs: int64(i + 1),
			Error:     errMessage,
			CheckedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("recordNodeHealthResult(%d) error = %v", i, err)
		}
	}
	if err := flushNodeHealthBatcher(ctx); err != nil {
		t.Fatalf("flushNodeHealthBatcher() error = %v", err)
	}

	var historyCount int64
	if err := model.GetTx(nil).Model(&tables.ProxyNodeHealthHistoryTable{}).
		Where("node_id = ?", node.ID).
		Count(&historyCount).Error; err != nil {
		t.Fatalf("count history error = %v", err)
	}
	if historyCount != nodeHealthHistoryLimit {
		t.Fatalf("history count = %d, want %d", historyCount, nodeHealthHistoryLimit)
	}

	health, err := getNodeHealth(ctx, nil, node.ID)
	if err != nil {
		t.Fatalf("getNodeHealth() error = %v", err)
	}
	if health == nil {
		t.Fatalf("health = nil")
	}
	if health.SuccessCount != 20 || health.FailureCount != 10 {
		t.Fatalf("health counts = success %d failure %d, want 20/10", health.SuccessCount, health.FailureCount)
	}
	if health.LastLatencyMs != 31 || health.Available {
		t.Fatalf("latest health = latency %d available %v, want 31/false", health.LastLatencyMs, health.Available)
	}
	if health.Blacklisted {
		t.Fatalf("health blacklisted = true, want false")
	}
}

func TestRecordNodeHealthResultBlacklistsAfterThreeFailuresAndReleaseResetsStreak(t *testing.T) {
	initProxyInMemoryDB(t)

	ctx := context.Background()
	port := uint16(1080)
	node, err := NodeCreate(ctx, nil, NodeUpsertRequest{
		Name:     "edge",
		Protocol: ProtocolHTTP,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}

	base := time.Now().Add(-time.Minute)
	for i := 0; i < 3; i++ {
		if _, err := recordNodeHealthResult(ctx, nil, node.ID, nodeHealthResultRecord{
			Source:    nodeHealthSourceNodeTest,
			TargetID:  node.ID,
			Available: false,
			Error:     "probe failed",
			CheckedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("record failure %d error = %v", i, err)
		}
	}

	health, err := getNodeHealth(ctx, nil, node.ID)
	if err != nil {
		t.Fatalf("getNodeHealth() error = %v", err)
	}
	if health == nil || !health.Blacklisted || health.ConsecutiveFailureCount != 3 {
		t.Fatalf("health = %+v, want blacklisted with 3 consecutive failures", health)
	}

	if _, err := NodeRelease(ctx, node.ID); err != nil {
		t.Fatalf("NodeRelease() error = %v", err)
	}
	health, err = recordNodeHealthResult(ctx, nil, node.ID, nodeHealthResultRecord{
		Source:    nodeHealthSourceNodeTest,
		TargetID:  node.ID,
		Available: false,
		Error:     "probe failed again",
		CheckedAt: base.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatalf("record failure after release error = %v", err)
	}
	if health.Blacklisted || health.ConsecutiveFailureCount != 1 {
		t.Fatalf("health after release failure = %+v, want not blacklisted with streak 1", health)
	}
	if health.FailureCount != 4 {
		t.Fatalf("recent failure count = %d, want 4", health.FailureCount)
	}
}
