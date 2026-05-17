package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"
)

const (
	healthProbeQueueSize = 10000
	healthProbeBatchSize = 256
)

type healthRunnerState struct {
	cancel context.CancelFunc
	done   chan struct{}
	config utils.ProxyHealthConfig
	queue  chan string
}

var (
	healthRunnerMu sync.Mutex
	healthRunner   *healthRunnerState
)

func HealthStart(ctx context.Context, cfg utils.ProxyHealthConfig) {
	if !cfg.Enabled {
		HealthStop()
		return
	}
	cfg = normalizeHealthConfig(cfg)
	if ctx == nil {
		ctx = context.Background()
	}

	HealthStop()

	runnerCtx, cancel := context.WithCancel(ctx)
	runner := &healthRunnerState{
		cancel: cancel,
		done:   make(chan struct{}),
		config: cfg,
		queue:  make(chan string, healthProbeQueueSize),
	}
	healthRunnerMu.Lock()
	healthRunner = runner
	healthRunnerMu.Unlock()

	go runHealthLoop(runnerCtx, runner)
}

func HealthStop() {
	healthRunnerMu.Lock()
	runner := healthRunner
	healthRunner = nil
	healthRunnerMu.Unlock()

	if runner == nil {
		return
	}
	runner.cancel()
	<-runner.done
}

func NodeHealthList(ctx context.Context, tx model.DBTx) ([]*tables.ProxyNodeHealthTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var rows []*tables.ProxyNodeHealthTable
	if err := tx.Order("updated_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func NodeHealthMap(ctx context.Context, tx model.DBTx, nodeIDs []string) map[string]*tables.ProxyNodeHealthTable {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return map[string]*tables.ProxyNodeHealthTable{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var rows []*tables.ProxyNodeHealthTable
	if err := tx.Where("node_id IN ?", nodeIDs).Find(&rows).Error; err != nil {
		utils.Logger.Warn("查询节点健康状态失败", zap.Error(err))
		return map[string]*tables.ProxyNodeHealthTable{}
	}
	result := make(map[string]*tables.ProxyNodeHealthTable, len(rows))
	for _, row := range rows {
		if row != nil {
			result[row.NodeID] = row
		}
	}
	return result
}

func NodeProbe(ctx context.Context, nodeID string) (*tables.ProxyNodeHealthTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	node, err := NodeGet(ctx, nil, nodeID)
	if err != nil {
		return nil, err
	}
	enqueueHealthProbeIDs([]string{node.ID})
	row, err := getNodeHealth(ctx, nil, node.ID)
	if err != nil {
		return nil, err
	}
	if row != nil {
		return row, nil
	}
	return &tables.ProxyNodeHealthTable{NodeID: node.ID}, nil
}

func NodeProbeAll(ctx context.Context) (*NodeHealthProbeAllResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	nodes, err := NodeList(ctx, nil)
	if err != nil {
		return nil, err
	}
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			nodeIDs = append(nodeIDs, node.ID)
		}
	}
	queued := enqueueHealthProbeIDs(nodeIDs)
	return &NodeHealthProbeAllResult{
		Total:  len(nodeIDs),
		Queued: queued,
		Failed: len(nodeIDs) - queued,
	}, nil
}

func NodeRelease(ctx context.Context, nodeID string) (*tables.ProxyNodeHealthTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := NodeGet(ctx, nil, nodeID); err != nil {
		return nil, err
	}

	now := time.Now()
	updates := map[string]any{
		"node_id":           nodeID,
		"blacklisted":       false,
		"blacklisted_until": nil,
		"failure_count":     0,
		"last_error":        "",
		"updated_at":        now,
	}
	return upsertNodeHealth(ctx, nil, nodeID, updates)
}

func NodeBlacklist(ctx context.Context, nodeID string, duration time.Duration) (*tables.ProxyNodeHealthTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if duration <= 0 {
		duration = normalizeHealthConfig(currentHealthConfig()).BlacklistDuration
	}
	if duration <= 0 {
		return nil, ErrInvalidHealthDuration
	}
	if _, err := NodeGet(ctx, nil, nodeID); err != nil {
		return nil, err
	}

	now := time.Now()
	until := now.Add(duration)
	updates := map[string]any{
		"node_id":           nodeID,
		"available":         false,
		"blacklisted":       true,
		"blacklisted_until": &until,
		"last_error":        "manually blacklisted",
		"updated_at":        now,
	}
	return upsertNodeHealth(ctx, nil, nodeID, updates)
}

func blacklistRuntimeExcludedNode(ctx context.Context, node *tables.ProxyNodeTable, runtimeErr error) (*tables.ProxyNodeHealthTable, error) {
	if node == nil {
		return nil, ErrNodeNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := normalizeHealthConfig(currentHealthConfig())
	now := time.Now()
	until := now.Add(cfg.BlacklistDuration)
	updates := map[string]any{
		"node_id":           node.ID,
		"available":         false,
		"blacklisted":       true,
		"blacklisted_until": &until,
		"failure_count":     cfg.FailureThreshold,
		"last_error":        errorString(runtimeErr),
		"last_failure_at":   &now,
		"updated_at":        now,
	}
	return upsertNodeHealth(ctx, nil, node.ID, updates)
}

func nodeHealthBlacklistedIDs(ctx context.Context, tx model.DBTx) (map[string]struct{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	now := time.Now()
	if err := tx.Model(&tables.ProxyNodeHealthTable{}).
		Where("blacklisted = ? AND blacklisted_until IS NOT NULL AND blacklisted_until <= ?", true, now).
		Updates(map[string]any{
			"blacklisted":       false,
			"blacklisted_until": nil,
			"failure_count":     0,
			"updated_at":        now,
		}).Error; err != nil {
		return nil, err
	}

	var rows []*tables.ProxyNodeHealthTable
	if err := tx.Where("blacklisted = ? AND (blacklisted_until IS NULL OR blacklisted_until > ?)", true, now).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row != nil && row.NodeID != "" {
			result[row.NodeID] = struct{}{}
		}
	}
	return result, nil
}

func runHealthLoop(ctx context.Context, runner *healthRunnerState) {
	defer close(runner.done)

	for {
		select {
		case <-ctx.Done():
			return
		case nodeID := <-runner.queue:
			probeNodeIDsWithLog(ctx, runner.config, drainHealthProbeBatch(runner.queue, nodeID))
		}
	}
}

func drainHealthProbeBatch(queue <-chan string, first string) []string {
	nodeIDs := []string{first}
	for len(nodeIDs) < healthProbeBatchSize {
		select {
		case nodeID := <-queue:
			nodeIDs = append(nodeIDs, nodeID)
		default:
			return uniqueNonEmpty(nodeIDs)
		}
	}
	return uniqueNonEmpty(nodeIDs)
}

func enqueueHealthProbeIDs(nodeIDs []string) int {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return 0
	}

	healthRunnerMu.Lock()
	runner := healthRunner
	healthRunnerMu.Unlock()
	if runner == nil {
		cfg := currentHealthConfig()
		go probeNodeIDsWithLog(context.Background(), cfg, nodeIDs)
		return len(nodeIDs)
	}

	queued := 0
	for _, nodeID := range nodeIDs {
		select {
		case runner.queue <- nodeID:
			queued++
		default:
			utils.Logger.Warn("节点健康探测队列已满", zap.Int("queued", queued), zap.Int("dropped", len(nodeIDs)-queued))
			return queued
		}
	}
	return queued
}

func probeNodeIDsWithLog(ctx context.Context, cfg utils.ProxyHealthConfig, nodeIDs []string) {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return
	}
	nodes, err := findNodesByIDs(ctx, nil, nodeIDs)
	if err != nil {
		utils.Logger.Warn("加载节点健康探测列表失败", zap.Error(err))
		return
	}
	probeNodes(ctx, nil, nodes, cfg)
}

func probeNodes(ctx context.Context, tx model.DBTx, nodes []*tables.ProxyNodeTable, cfg utils.ProxyHealthConfig) []*tables.ProxyNodeHealthTable {
	cfg = normalizeHealthConfig(cfg)
	if cfg.MaxConcurrency <= 1 || len(nodes) <= 1 {
		rows := make([]*tables.ProxyNodeHealthTable, 0, len(nodes))
		for _, node := range nodes {
			row, err := probeAndSaveNode(ctx, tx, node, cfg, time.Now())
			if err != nil {
				utils.Logger.Warn("节点健康探测失败", zap.String("nodeId", nodeIDForLog(node)), zap.Error(err))
				continue
			}
			rows = append(rows, row)
		}
		return rows
	}

	type probeResult struct {
		row *tables.ProxyNodeHealthTable
		err error
		id  string
	}
	results := make(chan probeResult, len(nodes))
	jobs := make(chan *tables.ProxyNodeTable)
	var wg sync.WaitGroup

	workers := cfg.MaxConcurrency
	if workers > len(nodes) {
		workers = len(nodes)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range jobs {
				row, err := probeAndSaveNode(ctx, tx, node, cfg, time.Now())
				results <- probeResult{row: row, err: err, id: node.ID}
			}
		}()
	}

	sent := 0
	for _, node := range nodes {
		if node == nil {
			continue
		}
		select {
		case jobs <- node:
			sent++
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			close(results)
			rows := make([]*tables.ProxyNodeHealthTable, 0, len(nodes))
			for result := range results {
				if result.row != nil {
					rows = append(rows, result.row)
				}
			}
			return rows
		}
	}
	close(jobs)
	wg.Wait()
	close(results)

	rows := make([]*tables.ProxyNodeHealthTable, 0, sent)
	for result := range results {
		if result.err != nil {
			utils.Logger.Warn("节点健康探测失败", zap.String("nodeId", result.id), zap.Error(result.err))
			continue
		}
		rows = append(rows, result.row)
	}
	return rows
}

func probeAndSaveNode(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, cfg utils.ProxyHealthConfig, now time.Time) (*tables.ProxyNodeHealthTable, error) {
	if node == nil {
		return nil, ErrNodeNotFound
	}
	cfg = normalizeHealthConfig(cfg)

	existing, err := getNodeHealth(ctx, tx, node.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil && isHealthBlacklisted(existing, now) {
		return existing, nil
	}

	started := time.Now()
	probeErr := probeNode(ctx, node, cfg)
	latencyMs := time.Since(started).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}

	updates := map[string]any{
		"node_id":         node.ID,
		"last_checked_at": &now,
		"last_latency_ms": latencyMs,
		"updated_at":      now,
	}
	failureCount := 0
	if existing != nil {
		failureCount = existing.FailureCount
	}
	if probeErr == nil {
		successCount := int64(1)
		if existing != nil {
			successCount = existing.SuccessCount + 1
		}
		updates["available"] = true
		updates["failure_count"] = 0
		updates["success_count"] = successCount
		updates["blacklisted"] = false
		updates["blacklisted_until"] = nil
		updates["last_error"] = ""
		updates["last_success_at"] = &now
	} else {
		failureCount++
		updates["available"] = false
		updates["failure_count"] = failureCount
		updates["last_error"] = probeErr.Error()
		updates["last_failure_at"] = &now
		if cfg.FailureThreshold > 0 && failureCount >= cfg.FailureThreshold {
			until := now.Add(cfg.BlacklistDuration)
			updates["blacklisted"] = true
			updates["blacklisted_until"] = &until
		}
	}
	return upsertNodeHealth(ctx, tx, node.ID, updates)
}

func probeNode(ctx context.Context, node *tables.ProxyNodeTable, cfg utils.ProxyHealthConfig) error {
	if node == nil {
		return ErrNodeNotFound
	}
	probeURL := cfg.ProbeURL
	if probeURL == "" {
		probeURL = utils.DefaultProxyHealthConfig().ProbeURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = utils.DefaultProxyHealthConfig().Timeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	proxyURL, instance, err := startHealthProbeProxy(probeCtx, node)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := instance.Close(); closeErr != nil {
			utils.Logger.Warn("关闭健康探测 sing-box 实例失败", zap.String("nodeId", node.ID), zap.Error(closeErr))
		}
	}()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("probe status %d", resp.StatusCode)
	}
	return nil
}

func startHealthProbeProxy(ctx context.Context, node *tables.ProxyNodeTable) (*url.URL, *box.Box, error) {
	outboundTag := "health-node-" + node.ID
	outbound, err := buildNodeOutbound(node, outboundTag)
	if err != nil {
		return nil, nil, err
	}
	listenPort, err := reserveHealthProbePort()
	if err != nil {
		return nil, nil, err
	}
	listen, err := parseListenAddr("127.0.0.1")
	if err != nil {
		return nil, nil, err
	}

	inboundTag := "health-in-" + node.ID
	options := option.Options{
		Log: &option.LogOptions{
			Level:        "error",
			Timestamp:    true,
			DisableColor: true,
		},
		Inbounds: []option.Inbound{
			{
				Type: constant.TypeMixed,
				Tag:  inboundTag,
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     listen,
						ListenPort: listenPort,
					},
				},
			},
		},
		Outbounds: []option.Outbound{
			{
				Type:    constant.TypeDirect,
				Tag:     constant.TypeDirect,
				Options: &option.DirectOutboundOptions{},
			},
			{
				Type:    constant.TypeBlock,
				Tag:     constant.TypeBlock,
				Options: &option.StubOptions{},
			},
			outbound,
		},
		Route: &option.RouteOptions{
			Rules: []option.Rule{buildInboundRouteRule(inboundTag, outboundTag)},
			Final: constant.TypeBlock,
		},
	}
	instance, err := box.New(box.Options{
		Options: options,
		Context: singBoxContext(ctx),
	})
	if err != nil {
		return nil, nil, err
	}
	if err := instance.Start(); err != nil {
		_ = instance.Close()
		return nil, nil, err
	}
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", listenPort))
	if err != nil {
		_ = instance.Close()
		return nil, nil, err
	}
	return proxyURL, instance, nil
}

func reserveHealthProbePort() (uint16, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 || addr.Port > 65535 {
		return 0, ErrInvalidPort
	}
	return uint16(addr.Port), nil
}

func getNodeHealth(ctx context.Context, tx model.DBTx, nodeID string) (*tables.ProxyNodeHealthTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	var row tables.ProxyNodeHealthTable
	if err := tx.Where("node_id = ?", nodeID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func upsertNodeHealth(ctx context.Context, tx model.DBTx, nodeID string, updates map[string]any) (*tables.ProxyNodeHealthTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	if updates == nil {
		updates = map[string]any{}
	}
	updates["node_id"] = nodeID
	now := time.Now()
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = now
	}

	row, err := getNodeHealth(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		row = &tables.ProxyNodeHealthTable{NodeID: nodeID}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "node_id"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(row).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Model(&tables.ProxyNodeHealthTable{}).Where("node_id = ?", nodeID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return getNodeHealth(ctx, tx, nodeID)
}

func isHealthBlacklisted(row *tables.ProxyNodeHealthTable, now time.Time) bool {
	if row == nil || !row.Blacklisted {
		return false
	}
	return row.BlacklistedUntil == nil || row.BlacklistedUntil.After(now)
}

func currentHealthConfig() utils.ProxyHealthConfig {
	healthRunnerMu.Lock()
	defer healthRunnerMu.Unlock()

	if healthRunner != nil {
		return healthRunner.config
	}
	return utils.DefaultProxyHealthConfig()
}

func normalizeHealthConfig(cfg utils.ProxyHealthConfig) utils.ProxyHealthConfig {
	defaults := utils.DefaultProxyHealthConfig()
	if cfg.ProbeURL == "" {
		cfg.ProbeURL = defaults.ProbeURL
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaults.Interval
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaults.FailureThreshold
	}
	if cfg.BlacklistDuration <= 0 {
		cfg.BlacklistDuration = defaults.BlacklistDuration
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = defaults.MaxConcurrency
	}
	return cfg
}

func nodeIDForLog(node *tables.ProxyNodeTable) string {
	if node == nil {
		return ""
	}
	return node.ID
}
