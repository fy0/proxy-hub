package proxy

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"
)

const (
	nodeHealthFlushInterval  = 30 * time.Second
	nodeHealthFlushBatchSize = 256
)

type nodeHealthHistoryWindowEntry struct {
	Source    string
	TargetID  string
	ProbeURL  string
	Available bool
	LatencyMs int64
	Error     string
	CheckedAt time.Time
}

type nodeHealthMemoryState struct {
	snapshot *tables.ProxyNodeHealthTable
	history  []nodeHealthHistoryWindowEntry
	dirty    bool
	revision uint64
}

type nodeHealthPersistPayload struct {
	nodeID   string
	revision uint64
	snapshot *tables.ProxyNodeHealthTable
	history  []*tables.ProxyNodeHealthHistoryTable
	exists   bool
}

type nodeHealthBatcher struct {
	startMu sync.Mutex
	loadMu  sync.Mutex
	flushMu sync.Mutex

	mu sync.RWMutex

	started     bool
	loaded      bool
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	flushOnStop bool

	states map[string]*nodeHealthMemoryState
}

var globalNodeHealthBatcher = &nodeHealthBatcher{
	states: map[string]*nodeHealthMemoryState{},
}

func (b *nodeHealthBatcher) ensureLoaded(ctx context.Context) error {
	b.loadMu.Lock()
	defer b.loadMu.Unlock()

	if b.loaded {
		return nil
	}

	db := model.GetDB()
	if db == nil {
		return model.ErrDBNotReady
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var snapshots []*tables.ProxyNodeHealthTable
	if err := db.WithContext(ctx).
		Order("updated_at DESC").
		Find(&snapshots).Error; err != nil {
		return err
	}

	var historyRows []*tables.ProxyNodeHealthHistoryTable
	if err := db.WithContext(ctx).
		Order("node_id ASC, checked_at ASC, created_at ASC, id ASC").
		Find(&historyRows).Error; err != nil {
		return err
	}

	loadedStates := make(map[string]*nodeHealthMemoryState, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot == nil || strings.TrimSpace(snapshot.NodeID) == "" {
			continue
		}
		loadedStates[snapshot.NodeID] = &nodeHealthMemoryState{
			snapshot: cloneNodeHealthSnapshot(snapshot),
		}
	}
	for _, row := range historyRows {
		if row == nil || strings.TrimSpace(row.NodeID) == "" {
			continue
		}
		state := loadedStates[row.NodeID]
		if state == nil {
			state = &nodeHealthMemoryState{
				snapshot: &tables.ProxyNodeHealthTable{NodeID: row.NodeID},
			}
			loadedStates[row.NodeID] = state
		}
		state.history = append(state.history, nodeHealthHistoryWindowEntry{
			Source:    row.Source,
			TargetID:  row.TargetID,
			ProbeURL:  row.ProbeURL,
			Available: row.Available,
			LatencyMs: row.LatencyMs,
			Error:     row.Error,
			CheckedAt: row.CheckedAt,
		})
		if len(state.history) > nodeHealthHistoryLimit {
			state.history = append([]nodeHealthHistoryWindowEntry(nil), state.history[len(state.history)-nodeHealthHistoryLimit:]...)
		}
	}

	b.mu.Lock()
	b.states = loadedStates
	b.loaded = true
	b.mu.Unlock()
	return nil
}

func (b *nodeHealthBatcher) ensureStarted() {
	b.startMu.Lock()
	defer b.startMu.Unlock()

	if b.started {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	b.ctx = ctx
	b.cancel = cancel
	b.done = done
	b.started = true
	b.flushOnStop = true

	go b.run(ctx, done)
}

func (b *nodeHealthBatcher) run(ctx context.Context, done chan struct{}) {
	ticker := time.NewTicker(nodeHealthFlushInterval)
	defer ticker.Stop()
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			b.startMu.Lock()
			flushOnStop := b.flushOnStop
			b.startMu.Unlock()
			if flushOnStop {
				flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = b.flush(flushCtx)
				cancel()
			}
			return
		case <-ticker.C:
			flushCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = b.flush(flushCtx)
			cancel()
		}
	}
}

func (b *nodeHealthBatcher) shutdown(flush bool) {
	b.startMu.Lock()
	cancel := b.cancel
	done := b.done
	started := b.started
	b.flushOnStop = flush
	b.cancel = nil
	b.done = nil
	b.ctx = nil
	b.started = false
	b.startMu.Unlock()

	if started && cancel != nil {
		if flush {
			cancel()
			<-done
		} else {
			cancel()
			<-done
		}
	} else if flush {
		flushCtx, cancelFlush := context.WithTimeout(context.Background(), 5*time.Second)
		_ = b.flush(flushCtx)
		cancelFlush()
	}

	b.loadMu.Lock()
	b.loaded = false
	b.loadMu.Unlock()

	b.mu.Lock()
	b.states = map[string]*nodeHealthMemoryState{}
	b.mu.Unlock()
}

func (b *nodeHealthBatcher) discard() {
	b.shutdown(false)
}

func (b *nodeHealthBatcher) stop() {
	b.shutdown(true)
}

func (b *nodeHealthBatcher) list(ctx context.Context) ([]*tables.ProxyNodeHealthTable, error) {
	if err := b.ensureLoaded(ctx); err != nil {
		return nil, err
	}

	b.mu.RLock()
	rows := make([]*tables.ProxyNodeHealthTable, 0, len(b.states))
	nodeIDs := make([]string, 0, len(b.states))
	for nodeID, state := range b.states {
		if state == nil || state.snapshot == nil || strings.TrimSpace(nodeID) == "" {
			continue
		}
		nodeIDs = append(nodeIDs, nodeID)
		rows = append(rows, cloneNodeHealthSnapshotForRead(state.snapshot))
	}
	b.mu.RUnlock()

	existing, err := existingNodeIDSet(ctx, nodeIDs)
	if err != nil {
		return nil, err
	}

	filtered := rows[:0]
	for _, row := range rows {
		if row == nil {
			continue
		}
		if _, ok := existing[row.NodeID]; ok {
			filtered = append(filtered, row)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	return filtered, nil
}

func (b *nodeHealthBatcher) mapByNodeIDs(ctx context.Context, nodeIDs []string) (map[string]*tables.ProxyNodeHealthTable, error) {
	if err := b.ensureLoaded(ctx); err != nil {
		return nil, err
	}

	result := make(map[string]*tables.ProxyNodeHealthTable, len(nodeIDs))
	b.mu.RLock()
	for _, nodeID := range nodeIDs {
		state := b.states[nodeID]
		if state == nil || state.snapshot == nil {
			continue
		}
		result[nodeID] = cloneNodeHealthSnapshotForRead(state.snapshot)
	}
	b.mu.RUnlock()
	return result, nil
}

func (b *nodeHealthBatcher) get(ctx context.Context, nodeID string) (*tables.ProxyNodeHealthTable, error) {
	if err := b.ensureLoaded(ctx); err != nil {
		return nil, err
	}

	b.mu.RLock()
	state := b.states[nodeID]
	var snapshot *tables.ProxyNodeHealthTable
	if state != nil && state.snapshot != nil {
		snapshot = cloneNodeHealthSnapshotForRead(state.snapshot)
	}
	b.mu.RUnlock()
	return snapshot, nil
}

func (b *nodeHealthBatcher) recordProbeResult(ctx context.Context, nodeID string, record nodeHealthResultRecord) (*tables.ProxyNodeHealthTable, error) {
	if err := b.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	b.ensureStarted()

	checkedAt := record.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now()
	}
	if record.LatencyMs < 0 {
		record.LatencyMs = 0
	}
	record.CheckedAt = checkedAt
	record.Source = strings.TrimSpace(record.Source)
	if record.Source == "" {
		record.Source = nodeHealthSourceNodeProbe
	}
	record.TargetID = strings.TrimSpace(record.TargetID)
	record.ProbeURL = strings.TrimSpace(record.ProbeURL)
	record.Error = strings.TrimSpace(record.Error)

	b.mu.Lock()
	state := ensureNodeHealthMemoryState(b.states, nodeID)
	applyNodeHealthProbeRecord(state, nodeID, record, normalizeHealthConfig(currentHealthConfig()))
	result := cloneNodeHealthSnapshotForRead(state.snapshot)
	b.mu.Unlock()
	return result, nil
}

func (b *nodeHealthBatcher) updateSnapshot(ctx context.Context, nodeID string, updateFn func(snapshot *tables.ProxyNodeHealthTable, now time.Time)) (*tables.ProxyNodeHealthTable, error) {
	if err := b.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	b.ensureStarted()

	now := time.Now()

	b.mu.Lock()
	state := ensureNodeHealthMemoryState(b.states, nodeID)
	snapshot := state.snapshot
	if snapshot == nil {
		snapshot = &tables.ProxyNodeHealthTable{NodeID: nodeID}
		state.snapshot = snapshot
	}
	if snapshot.NodeID == "" {
		snapshot.NodeID = nodeID
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	updateFn(snapshot, now)
	snapshot.UpdatedAt = now
	state.dirty = true
	state.revision++
	result := cloneNodeHealthSnapshotForRead(snapshot)
	b.mu.Unlock()
	return result, nil
}

func (b *nodeHealthBatcher) reviveNodes(ctx context.Context, nodeIDs []string) error {
	if err := b.ensureLoaded(ctx); err != nil {
		return err
	}
	b.ensureStarted()

	now := time.Now()

	b.mu.Lock()
	for _, nodeID := range uniqueNonEmpty(nodeIDs) {
		state := ensureNodeHealthMemoryState(b.states, nodeID)
		snapshot := state.snapshot
		if snapshot == nil {
			snapshot = &tables.ProxyNodeHealthTable{NodeID: nodeID}
			state.snapshot = snapshot
		}
		if snapshot.CreatedAt.IsZero() {
			snapshot.CreatedAt = now
		}
		snapshot.NodeID = nodeID
		snapshot.Available = true
		snapshot.Blacklisted = false
		snapshot.BlacklistedUntil = nil
		snapshot.ConsecutiveFailureCount = 0
		snapshot.LastError = ""
		snapshot.UpdatedAt = now
		state.dirty = true
		state.revision++
	}
	b.mu.Unlock()
	return nil
}

func (b *nodeHealthBatcher) blacklistedIDs(ctx context.Context) (map[string]struct{}, error) {
	if err := b.ensureLoaded(ctx); err != nil {
		return nil, err
	}

	now := time.Now()
	result := map[string]struct{}{}

	b.mu.RLock()
	for nodeID, state := range b.states {
		if state == nil || state.snapshot == nil {
			continue
		}
		if isHealthBlacklisted(state.snapshot, now) {
			result[nodeID] = struct{}{}
		}
	}
	b.mu.RUnlock()
	return result, nil
}

func (b *nodeHealthBatcher) flush(ctx context.Context) error {
	if err := b.ensureLoaded(ctx); err != nil {
		return err
	}

	b.flushMu.Lock()
	defer b.flushMu.Unlock()

	b.mu.RLock()
	payloads := make([]nodeHealthPersistPayload, 0, len(b.states))
	nodeIDs := make([]string, 0, len(b.states))
	for nodeID, state := range b.states {
		if state == nil || !state.dirty || state.snapshot == nil || strings.TrimSpace(nodeID) == "" {
			continue
		}
		payloads = append(payloads, nodeHealthPersistPayload{
			nodeID:   nodeID,
			revision: state.revision,
			snapshot: cloneNodeHealthSnapshot(state.snapshot),
			history:  historyEntriesToRows(nodeID, state.history),
		})
		nodeIDs = append(nodeIDs, nodeID)
	}
	b.mu.RUnlock()

	if len(payloads) == 0 {
		return nil
	}

	existing, err := existingNodeIDSet(ctx, nodeIDs)
	if err != nil {
		return err
	}
	for index := range payloads {
		_, payloads[index].exists = existing[payloads[index].nodeID]
	}

	snapshots := make([]*tables.ProxyNodeHealthTable, 0, len(payloads))
	historyRows := make([]*tables.ProxyNodeHealthHistoryTable, 0, len(payloads)*nodeHealthHistoryLimit)
	existingNodeIDs := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		if !payload.exists {
			continue
		}
		existingNodeIDs = append(existingNodeIDs, payload.nodeID)
		snapshots = append(snapshots, payload.snapshot)
		historyRows = append(historyRows, payload.history...)
	}

	if len(existingNodeIDs) > 0 {
		if err := model.Transaction(ctx, func(tx model.DBTx) error {
			if err := tx.WithContext(ctx).
				Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "node_id"}},
					DoUpdates: clause.AssignmentColumns([]string{
						"available",
						"failure_count",
						"success_count",
						"consecutive_failure_count",
						"blacklisted",
						"blacklisted_until",
						"last_latency_ms",
						"last_error",
						"last_checked_at",
						"last_success_at",
						"last_failure_at",
						"updated_at",
					}),
				}).
				CreateInBatches(snapshots, nodeHealthFlushBatchSize).Error; err != nil {
				return err
			}
			if err := tx.WithContext(ctx).
				Where("node_id IN ?", existingNodeIDs).
				Unscoped().
				Delete(&tables.ProxyNodeHealthHistoryTable{}).Error; err != nil {
				return err
			}
			if len(historyRows) == 0 {
				return nil
			}
			return tx.WithContext(ctx).
				CreateInBatches(historyRows, nodeHealthFlushBatchSize).Error
		}); err != nil {
			return err
		}
	}

	b.mu.Lock()
	for _, payload := range payloads {
		state := b.states[payload.nodeID]
		if state == nil || state.revision != payload.revision {
			continue
		}
		if !payload.exists {
			delete(b.states, payload.nodeID)
			continue
		}
		state.dirty = false
	}
	b.mu.Unlock()
	return nil
}

func ensureNodeHealthMemoryState(states map[string]*nodeHealthMemoryState, nodeID string) *nodeHealthMemoryState {
	state := states[nodeID]
	if state != nil {
		if state.snapshot == nil {
			state.snapshot = &tables.ProxyNodeHealthTable{NodeID: nodeID}
		}
		return state
	}
	state = &nodeHealthMemoryState{
		snapshot: &tables.ProxyNodeHealthTable{NodeID: nodeID},
	}
	states[nodeID] = state
	return state
}

func applyNodeHealthProbeRecord(state *nodeHealthMemoryState, nodeID string, record nodeHealthResultRecord, cfg utils.ProxyHealthConfig) {
	now := time.Now()
	snapshot := state.snapshot
	if snapshot == nil {
		snapshot = &tables.ProxyNodeHealthTable{NodeID: nodeID}
		state.snapshot = snapshot
	}
	if snapshot.NodeID == "" {
		snapshot.NodeID = nodeID
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}

	prevBlacklisted := snapshot.Blacklisted
	prevBlacklistedUntil := cloneTimePtr(snapshot.BlacklistedUntil)
	prevConsecutiveFailures := snapshot.ConsecutiveFailureCount

	state.history = append(state.history, nodeHealthHistoryWindowEntry{
		Source:    record.Source,
		TargetID:  record.TargetID,
		ProbeURL:  record.ProbeURL,
		Available: record.Available,
		LatencyMs: record.LatencyMs,
		Error:     record.Error,
		CheckedAt: record.CheckedAt,
	})
	if len(state.history) > nodeHealthHistoryLimit {
		state.history = append([]nodeHealthHistoryWindowEntry(nil), state.history[len(state.history)-nodeHealthHistoryLimit:]...)
	}

	successCount, failureCount, lastSuccessAt, lastFailureAt := summarizeNodeHealthHistoryWindow(state.history)

	consecutiveFailures := prevConsecutiveFailures
	if record.Available {
		consecutiveFailures = 0
	} else {
		consecutiveFailures++
	}

	snapshot.Available = record.Available
	snapshot.FailureCount = failureCount
	snapshot.SuccessCount = successCount
	snapshot.ConsecutiveFailureCount = consecutiveFailures
	snapshot.LastCheckedAt = cloneTimePtr(&record.CheckedAt)
	snapshot.LastLatencyMs = record.LatencyMs
	snapshot.LastSuccessAt = cloneTimePtr(lastSuccessAt)
	snapshot.LastFailureAt = cloneTimePtr(lastFailureAt)
	snapshot.UpdatedAt = now

	if record.Available {
		snapshot.Blacklisted = false
		snapshot.BlacklistedUntil = nil
		snapshot.LastError = ""
	} else if cfg.FailureThreshold > 0 && consecutiveFailures >= cfg.FailureThreshold {
		until := record.CheckedAt.Add(cfg.BlacklistDuration)
		snapshot.Blacklisted = true
		snapshot.BlacklistedUntil = &until
		snapshot.LastError = record.Error
	} else if prevBlacklisted && (prevBlacklistedUntil == nil || prevBlacklistedUntil.After(record.CheckedAt)) {
		snapshot.Blacklisted = true
		snapshot.BlacklistedUntil = prevBlacklistedUntil
		snapshot.LastError = record.Error
	} else {
		snapshot.Blacklisted = false
		snapshot.BlacklistedUntil = nil
		snapshot.LastError = record.Error
	}

	state.dirty = true
	state.revision++
}

func summarizeNodeHealthHistoryWindow(history []nodeHealthHistoryWindowEntry) (int64, int, *time.Time, *time.Time) {
	var successCount int64
	failureCount := 0
	var lastSuccessAt *time.Time
	var lastFailureAt *time.Time
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		checkedAt := entry.CheckedAt
		if entry.Available {
			successCount++
			if lastSuccessAt == nil {
				lastSuccessAt = &checkedAt
			}
			continue
		}
		failureCount++
		if lastFailureAt == nil {
			lastFailureAt = &checkedAt
		}
	}
	return successCount, failureCount, lastSuccessAt, lastFailureAt
}

func historyEntriesToRows(nodeID string, history []nodeHealthHistoryWindowEntry) []*tables.ProxyNodeHealthHistoryTable {
	rows := make([]*tables.ProxyNodeHealthHistoryTable, 0, len(history))
	for _, entry := range history {
		rows = append(rows, &tables.ProxyNodeHealthHistoryTable{
			NodeID:    nodeID,
			Source:    entry.Source,
			TargetID:  entry.TargetID,
			ProbeURL:  entry.ProbeURL,
			Available: entry.Available,
			LatencyMs: entry.LatencyMs,
			Error:     entry.Error,
			CheckedAt: entry.CheckedAt,
		})
	}
	return rows
}

func existingNodeIDSet(ctx context.Context, nodeIDs []string) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return result, nil
	}
	db := model.GetDB()
	if db == nil {
		return result, model.ErrDBNotReady
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var ids []string
	if err := db.WithContext(ctx).
		Model(&tables.ProxyNodeTable{}).
		Where("id IN ?", nodeIDs).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	for _, nodeID := range ids {
		if strings.TrimSpace(nodeID) != "" {
			result[nodeID] = struct{}{}
		}
	}
	return result, nil
}

func cloneNodeHealthSnapshot(snapshot *tables.ProxyNodeHealthTable) *tables.ProxyNodeHealthTable {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.BlacklistedUntil = cloneTimePtr(snapshot.BlacklistedUntil)
	cloned.LastCheckedAt = cloneTimePtr(snapshot.LastCheckedAt)
	cloned.LastSuccessAt = cloneTimePtr(snapshot.LastSuccessAt)
	cloned.LastFailureAt = cloneTimePtr(snapshot.LastFailureAt)
	return &cloned
}

func cloneNodeHealthSnapshotForRead(snapshot *tables.ProxyNodeHealthTable) *tables.ProxyNodeHealthTable {
	cloned := cloneNodeHealthSnapshot(snapshot)
	if cloned == nil {
		return nil
	}
	if cloned.Blacklisted && cloned.BlacklistedUntil != nil && !cloned.BlacklistedUntil.After(time.Now()) {
		cloned.Blacklisted = false
		cloned.BlacklistedUntil = nil
	}
	return cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func flushNodeHealthBatcher(ctx context.Context) error {
	return globalNodeHealthBatcher.flush(ctx)
}

func discardNodeHealthBatcher() {
	globalNodeHealthBatcher.discard()
}
