package singboxcore

import (
	"context"
	"time"
)

type ProbeResult struct {
	Alive     bool
	Latency   time.Duration
	Error     error
	CheckedAt time.Time
}

type Prober interface {
	Probe(ctx context.Context, node *NodeState) ProbeResult
}

type NoopProber struct{}

func (NoopProber) Probe(ctx context.Context, node *NodeState) ProbeResult {
	_ = ctx
	return ProbeResult{Alive: node != nil, CheckedAt: time.Now()}
}
