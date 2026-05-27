package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"go.uber.org/zap"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"
)

const (
	healthProbeQueueSize           = 10000
	healthProbeBatchSize           = 256
	nodeHealthHistoryLimit         = 30
	nodeHealthSourceNodeProbe      = "node-probe"
	nodeHealthSourceNodeTest       = "node-test"
	nodeHealthSourceMappingTest    = "mapping-test"
	nodeHealthSourceRuntimeProbe   = "runtime-probe"
	nodeHealthSourceRuntimeTraffic = "runtime-traffic"
)

type nodeHealthResultRecord struct {
	Source    string
	TargetID  string
	ProbeURL  string
	Available bool
	LatencyMs int64
	Error     string
	CheckedAt time.Time
}

type nodeProbeResult struct {
	err       error
	routePath []ProxyRouteHopDTO
}

type healthRunnerState struct {
	cancel context.CancelFunc
	done   chan struct{}
	config utils.ProxyHealthConfig
	queue  chan string
}

var (
	healthRunnerMu sync.Mutex
	healthRunner   *healthRunnerState

	healthProbeRoundRobinMu      sync.Mutex
	healthProbeRoundRobinOffsets = map[string]uint64{}
)

func HealthStart(ctx context.Context, cfg utils.ProxyHealthConfig) {
	if !cfg.Enabled {
		stopHealthRunner()
		return
	}
	cfg = normalizeHealthConfig(cfg)
	if ctx == nil {
		ctx = context.Background()
	}

	stopHealthRunner()

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
	stopHealthRunner()
	globalNodeHealthBatcher.stop()
}

func stopHealthRunner() {
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
	return globalNodeHealthBatcher.list(ctx)
}

func NodeHealthMap(ctx context.Context, tx model.DBTx, nodeIDs []string) map[string]*tables.ProxyNodeHealthTable {
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return map[string]*tables.ProxyNodeHealthTable{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := globalNodeHealthBatcher.mapByNodeIDs(ctx, nodeIDs)
	if err != nil {
		utils.Logger.Warn("查询节点健康状态失败", zap.Error(err))
		return map[string]*tables.ProxyNodeHealthTable{}
	}
	return rows
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

	return globalNodeHealthBatcher.updateSnapshot(ctx, nodeID, func(snapshot *tables.ProxyNodeHealthTable, now time.Time) {
		snapshot.Blacklisted = false
		snapshot.BlacklistedUntil = nil
		snapshot.ConsecutiveFailureCount = 0
		snapshot.LastError = ""
	})
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

	return globalNodeHealthBatcher.updateSnapshot(ctx, nodeID, func(snapshot *tables.ProxyNodeHealthTable, now time.Time) {
		until := now.Add(duration)
		snapshot.Available = false
		snapshot.Blacklisted = true
		snapshot.BlacklistedUntil = &until
		snapshot.ConsecutiveFailureCount = 0
		snapshot.LastError = "manually blacklisted"
	})
}

func recordNodeHealthResult(ctx context.Context, tx model.DBTx, nodeID string, record nodeHealthResultRecord) (*tables.ProxyNodeHealthTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(nodeID) == "" {
		return nil, ErrNodeNotFound
	}
	return globalNodeHealthBatcher.recordProbeResult(ctx, nodeID, record)
}

func NodeTest(ctx context.Context, nodeID string, req ProxyTestRequest) (*ProxyTestResultDTO, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	node, err := NodeGet(ctx, nil, nodeID)
	if err != nil {
		return nil, err
	}

	cfg := normalizeHealthConfig(currentHealthConfig())
	probeURL, err := normalizeProbeURL(req.ProbeURL, cfg.ProbeURL)
	if err != nil {
		return nil, err
	}
	cfg.ProbeURL = probeURL
	checkedAt := time.Now()
	health, routePath, err := probeAndSaveNodeForcedWithRoutePath(ctx, nil, node, cfg, checkedAt, true, nodeHealthSourceNodeTest)
	if err != nil {
		return nil, err
	}

	result := testResultFromHealth("node", node.ID, node.Name, cfg.ProbeURL, checkedAt, health)
	result.RoutePath = routePath
	if len(result.RoutePath) == 0 {
		result.RoutePath = testRoutePathForNode(ctx, nil, node)
	}
	result.Health = ToNodeHealthDTO(health)
	return result, nil
}

func MappingTest(ctx context.Context, mappingID string, req ProxyTestRequest) (*ProxyTestResultDTO, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mapping, err := MappingGet(ctx, nil, mappingID)
	if err != nil {
		return nil, err
	}

	checkedAt := time.Now()
	cfg := normalizeHealthConfig(currentHealthConfig())
	probeURL, err := normalizeProbeURL(req.ProbeURL, cfg.ProbeURL)
	if err != nil {
		return nil, err
	}

	result := &ProxyTestResultDTO{
		TargetType: "mapping",
		TargetID:   mapping.ID,
		TargetName: mappingRuntimeListen(mapping),
		ProbeURL:   probeURL,
		CheckedAt:  checkedAt,
	}
	if !mapping.Enabled {
		result.Error = "port mapping is disabled"
		return result, nil
	}

	status := RuntimeStatusGet()
	if failure := runtimeFailureForMapping(status, mapping.ID); failure != nil {
		result.Error = failure.Error
		return result, nil
	}
	if !runtimeHasInboundForMapping(status, mapping.ID) {
		result.Error = "port mapping runtime is not running"
		return result, nil
	}
	proxyURL, err := mappingProbeProxyURL(mapping)
	if err != nil {
		return nil, err
	}
	probeErr, latencyMs := executeHTTPProbe(ctx, probeURL, cfg.Timeout, proxyURL)
	status = RuntimeStatusGet()
	applyMappingTestRuntimeSelection(ctx, result, mapping, status)
	result.LatencyMs = latencyMs
	if probeErr != nil {
		result.Error = probeErr.Error()
		result.NodeError = firstNonEmpty(result.NodeError, runtimeNodeErrorFromProbe(result.NodeTag, result.Error))
		result.Health = saveMappingTestNodeHealth(ctx, result)
		return result, nil
	}
	result.Available = true
	result.Health = saveMappingTestNodeHealth(ctx, result)
	return result, nil
}

func applyMappingTestRuntimeSelection(ctx context.Context, result *ProxyTestResultDTO, mapping *tables.PortMappingTable, status RuntimeStatus) {
	if result == nil || mapping == nil {
		return
	}
	if node, ok := runtimeSelectedRouteNode(status, mapping.ID); ok {
		result.NodeName = node.NodeName
		result.NodeTag = node.NodeTag
		result.NodeError = node.Error
		if node.Kind == "node" {
			result.NodeID = node.NodeID
		}
	}
	result.RoutePath = testRoutePathForMapping(ctx, mapping, status)
	if result.NodeID == "" && result.NodeTag == "" {
		applyTestResultNodeFromRoutePath(result)
	}
}

func applyTestResultNodeFromRoutePath(result *ProxyTestResultDTO) {
	if result == nil {
		return
	}
	for index := len(result.RoutePath) - 1; index >= 0; index-- {
		hop := result.RoutePath[index]
		if hop.Kind != ChainMemberTypeNode || strings.TrimSpace(hop.ID) == "" {
			continue
		}
		result.NodeID = hop.ID
		result.NodeName = hop.Name
		result.NodeTag = hop.Tag
		return
	}
}

func saveMappingTestNodeHealth(ctx context.Context, result *ProxyTestResultDTO) *ProxyNodeHealthDTO {
	if result == nil || result.NodeID == "" {
		return nil
	}
	now := result.CheckedAt
	if now.IsZero() {
		now = time.Now()
	}
	health, err := recordNodeHealthResult(ctx, nil, result.NodeID, nodeHealthResultRecord{
		Source:    nodeHealthSourceMappingTest,
		TargetID:  result.TargetID,
		ProbeURL:  result.ProbeURL,
		Available: result.Available,
		LatencyMs: result.LatencyMs,
		Error:     firstNonEmpty(result.NodeError, result.Error),
		CheckedAt: now,
	})
	if err != nil {
		utils.Logger.Warn("本地端口测速写入节点健康状态失败",
			zap.String("mappingId", result.TargetID),
			zap.String("nodeId", result.NodeID),
			zap.Error(err),
		)
		return nil
	}
	return ToNodeHealthDTO(health)
}

func blacklistRuntimeExcludedNode(ctx context.Context, node *tables.ProxyNodeTable, runtimeErr error) (*tables.ProxyNodeHealthTable, error) {
	if node == nil {
		return nil, ErrNodeNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := normalizeHealthConfig(currentHealthConfig())
	return globalNodeHealthBatcher.updateSnapshot(ctx, node.ID, func(snapshot *tables.ProxyNodeHealthTable, now time.Time) {
		until := now.Add(cfg.BlacklistDuration)
		snapshot.Available = false
		snapshot.Blacklisted = true
		snapshot.BlacklistedUntil = &until
		snapshot.ConsecutiveFailureCount = 0
		snapshot.LastError = errorString(runtimeErr)
		snapshot.LastFailureAt = cloneTimePtr(&now)
	})
}

func nodeHealthBlacklistedIDs(ctx context.Context, tx model.DBTx) (map[string]struct{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return globalNodeHealthBatcher.blacklistedIDs(ctx)
}

func reviveNodeHealthIDs(ctx context.Context, tx model.DBTx, nodeIDs []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	nodeIDs = uniqueNonEmpty(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}
	return globalNodeHealthBatcher.reviveNodes(ctx, nodeIDs)
}

func recordRuntimeProbeResult(record singboxcore.ProbeRecord) {
	if strings.TrimSpace(record.NodeID) == "" {
		return
	}
	latencyMs := record.Latency.Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}
	_, err := recordNodeHealthResult(context.Background(), nil, record.NodeID, nodeHealthResultRecord{
		Source:    nodeHealthSourceRuntimeProbe,
		TargetID:  record.GroupTag,
		ProbeURL:  normalizeHealthConfig(currentHealthConfig()).ProbeURL,
		Available: record.Available,
		LatencyMs: latencyMs,
		Error:     record.Error,
		CheckedAt: record.CheckedAt,
	})
	if err != nil {
		utils.Logger.Warn("运行时周期测速写入节点健康状态失败",
			zap.String("groupTag", record.GroupTag),
			zap.String("nodeId", record.NodeID),
			zap.Error(err),
		)
	}
}

func recordRuntimeTrafficFailure(record singboxcore.TrafficFailureRecord) {
	record.NodeID = strings.TrimSpace(record.NodeID)
	if record.NodeID == "" {
		return
	}
	record.GroupTag = strings.TrimSpace(record.GroupTag)
	record.NodeTag = strings.TrimSpace(record.NodeTag)
	record.Stage = strings.TrimSpace(record.Stage)
	record.Error = strings.TrimSpace(record.Error)
	if record.CheckedAt.IsZero() {
		record.CheckedAt = time.Now()
	}
	go func() {
		if _, err := recordRuntimeTrafficFailureSync(record); err != nil {
			utils.Logger.Warn("真实代理流量失败写入节点健康状态失败",
				zap.String("groupTag", record.GroupTag),
				zap.String("nodeId", record.NodeID),
				zap.Error(err),
			)
		}
	}()
}

func recordRuntimeTrafficFailureSync(record singboxcore.TrafficFailureRecord) (*tables.ProxyNodeHealthTable, error) {
	cfg := normalizeHealthConfig(currentHealthConfig())
	errMessage := firstNonEmpty(record.Error, "traffic failed before first response byte")
	health, err := recordNodeHealthResult(context.Background(), nil, record.NodeID, nodeHealthResultRecord{
		Source:    nodeHealthSourceRuntimeTraffic,
		TargetID:  record.GroupTag,
		Available: false,
		Error:     errMessage,
		CheckedAt: record.CheckedAt,
	})
	if err != nil {
		return nil, err
	}
	if health == nil || !health.Blacklisted || cfg.FailureThreshold <= 0 || health.ConsecutiveFailureCount != cfg.FailureThreshold {
		return health, nil
	}
	if err := syncRuntimeMappingsForTrafficFailure(record); err != nil {
		utils.Logger.Warn("真实代理流量失败触发运行时同步失败",
			zap.String("groupTag", record.GroupTag),
			zap.String("nodeId", record.NodeID),
			zap.Error(err),
		)
	}
	return health, nil
}

func syncRuntimeMappingsForTrafficFailure(record singboxcore.TrafficFailureRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mappingIDs, err := RuntimeAffectedMappingIDsByNodes(ctx, []string{record.NodeID})
	if err != nil {
		return err
	}
	if len(mappingIDs) == 0 {
		return nil
	}
	_, err = RuntimeSyncMappings(ctx, mappingIDs)
	if err == nil {
		utils.Logger.Warn("真实代理流量连续失败，节点已进入黑名单并同步运行时",
			zap.String("groupTag", record.GroupTag),
			zap.String("nodeId", record.NodeID),
			zap.Strings("mappingIds", mappingIDs),
		)
	}
	return err
}

func reviveRuntimeBlacklistedNodes(event singboxcore.BlacklistRevivalEvent) {
	if err := reviveNodeHealthIDs(context.Background(), nil, event.NodeIDs); err != nil {
		utils.Logger.Warn("运行时黑名单兜底复活节点失败",
			zap.String("groupTag", event.GroupTag),
			zap.Strings("nodeIds", event.NodeIDs),
			zap.Error(err),
		)
		return
	}
	utils.Logger.Info("运行时黑名单兜底复活节点",
		zap.String("groupTag", event.GroupTag),
		zap.Strings("nodeIds", event.NodeIDs),
	)
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
	return probeAndSaveNodeForced(ctx, tx, node, cfg, now, false, nodeHealthSourceNodeProbe)
}

func probeAndSaveNodeForced(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, cfg utils.ProxyHealthConfig, now time.Time, force bool, source string) (*tables.ProxyNodeHealthTable, error) {
	health, _, err := probeAndSaveNodeForcedWithRoutePath(ctx, tx, node, cfg, now, force, source)
	return health, err
}

func probeAndSaveNodeForcedWithRoutePath(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, cfg utils.ProxyHealthConfig, now time.Time, force bool, source string) (*tables.ProxyNodeHealthTable, []ProxyRouteHopDTO, error) {
	if node == nil {
		return nil, nil, ErrNodeNotFound
	}
	cfg = normalizeHealthConfig(cfg)

	existing, err := getNodeHealth(ctx, tx, node.ID)
	if err != nil {
		return nil, nil, err
	}
	if !force && existing != nil && isHealthBlacklisted(existing, now) {
		return existing, nil, nil
	}

	started := time.Now()
	probeResult := probeNodeWithRoutePath(ctx, node, cfg)
	latencyMs := time.Since(started).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}
	errorMessage := ""
	if probeResult.err != nil {
		errorMessage = probeResult.err.Error()
	}
	health, err := recordNodeHealthResult(ctx, tx, node.ID, nodeHealthResultRecord{
		Source:    source,
		TargetID:  node.ID,
		ProbeURL:  cfg.ProbeURL,
		Available: probeResult.err == nil,
		LatencyMs: latencyMs,
		Error:     errorMessage,
		CheckedAt: now,
	})
	if err != nil {
		return nil, probeResult.routePath, err
	}
	return health, probeResult.routePath, nil
}

func probeNode(ctx context.Context, node *tables.ProxyNodeTable, cfg utils.ProxyHealthConfig) error {
	return probeNodeWithRoutePath(ctx, node, cfg).err
}

func probeNodeWithRoutePath(ctx context.Context, node *tables.ProxyNodeTable, cfg utils.ProxyHealthConfig) nodeProbeResult {
	if node == nil {
		return nodeProbeResult{err: ErrNodeNotFound}
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
		return nodeProbeResult{err: err}
	}
	defer func() {
		if closeErr := instance.Close(); closeErr != nil {
			utils.Logger.Warn("关闭健康探测 sing-box 实例失败", zap.String("nodeId", node.ID), zap.Error(closeErr))
		}
	}()

	probeErr, _ := executeHTTPProbe(probeCtx, probeURL, timeout, proxyURL)
	return nodeProbeResult{
		err:       probeErr,
		routePath: testRoutePathForNodeState(ctx, nil, node, instance.Snapshot()),
	}
}

func normalizeProbeURL(value string, fallback string) (string, error) {
	probeURL := strings.TrimSpace(value)
	if probeURL == "" {
		probeURL = strings.TrimSpace(fallback)
	}
	if probeURL == "" {
		probeURL = utils.DefaultProxyHealthConfig().ProbeURL
	}
	parsed, err := url.Parse(probeURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ErrInvalidProbeURL
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.String(), nil
	default:
		return "", ErrInvalidProbeURL
	}
}

func executeHTTPProbe(ctx context.Context, probeURL string, timeout time.Duration, proxyURL *url.URL) (error, int64) {
	if timeout <= 0 {
		timeout = utils.DefaultProxyHealthConfig().Timeout
	}
	probeURL, err := normalizeProbeURL(probeURL, "")
	if err != nil {
		return err, 0
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL, nil)
	if err != nil {
		return err, 0
	}
	transport := &http.Transport{}
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	started := time.Now()
	resp, err := client.Do(req)
	latencyMs := time.Since(started).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}
	if err != nil {
		return err, latencyMs
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("probe status %d", resp.StatusCode), latencyMs
	}
	return nil, latencyMs
}

func testResultFromHealth(targetType, targetID, targetName, probeURL string, checkedAt time.Time, health *tables.ProxyNodeHealthTable) *ProxyTestResultDTO {
	result := &ProxyTestResultDTO{
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		ProbeURL:   probeURL,
		CheckedAt:  checkedAt,
	}
	if health == nil {
		return result
	}
	result.Available = health.Available
	result.LatencyMs = health.LastLatencyMs
	result.Error = health.LastError
	if health.LastCheckedAt != nil {
		result.CheckedAt = *health.LastCheckedAt
	}
	if targetType == "node" {
		result.NodeID = targetID
		result.NodeName = targetName
		result.NodeTag = nodeOutboundTag(targetID)
		result.NodeError = health.LastError
	}
	return result
}

func testRoutePathForMapping(ctx context.Context, mapping *tables.PortMappingTable, status RuntimeStatus) []ProxyRouteHopDTO {
	if mapping == nil {
		return nil
	}
	route, ok := runtimeRouteForTag(status, mapping.ID, mappingOutboundTag(mapping.ID))
	if !ok {
		return nil
	}
	hops := make([]ProxyRouteHopDTO, 0)
	appendSelectedRouteHops(ctx, route, status.Routes, map[string]bool{}, &hops)
	return hops
}

func runtimeRouteForTag(status RuntimeStatus, mappingID string, groupTag string) (RuntimeRoute, bool) {
	for _, route := range status.Routes {
		if route.MappingID == mappingID && route.GroupTag == groupTag {
			return route, true
		}
	}
	return RuntimeRoute{}, false
}

func appendSelectedRouteHops(ctx context.Context, route RuntimeRoute, routes []RuntimeRoute, visited map[string]bool, hops *[]ProxyRouteHopDTO) {
	if visited[route.GroupTag] {
		return
	}
	visited[route.GroupTag] = true
	selected := selectedRuntimeRouteNode(route)
	if selected == nil {
		return
	}
	appendSelectedRuntimeNodeHops(ctx, route.MappingID, *selected, routes, visited, hops)
}

func appendSelectedRuntimeNodeHops(ctx context.Context, mappingID string, selected RuntimeRouteNode, routes []RuntimeRoute, visited map[string]bool, hops *[]ProxyRouteHopDTO) {
	if selected.Kind == "node" {
		if len(appendNodeRoutePath(ctx, nil, selected.NodeID, mappingID, routes, hops)) > 0 {
			return
		}
	}
	hop := routeHopFromRuntimeRouteNode(selected)
	hydrateRouteHopName(ctx, nil, &hop)
	*hops = append(*hops, hop)
	if selected.Kind != "group" {
		return
	}
	if childRoute, ok := runtimeRouteForTag(RuntimeStatus{Routes: routes}, mappingID, selected.NodeTag); ok {
		appendSelectedRouteHops(ctx, childRoute, routes, visited, hops)
	}
}

func appendNodeRoutePath(ctx context.Context, tx model.DBTx, nodeID string, mappingID string, routes []RuntimeRoute, hops *[]ProxyRouteHopDTO) []ProxyRouteHopDTO {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil
	}
	nodes, err := findNodesByIDs(ctx, tx, []string{nodeID})
	if err != nil || len(nodes) != 1 {
		return nil
	}
	nodePath := testRoutePathForNodeWithRuntimeRoutes(ctx, tx, nodes[0], mappingID, routes)
	if len(nodePath) == 0 {
		return nil
	}
	*hops = append(*hops, nodePath...)
	return nodePath
}

func selectedRuntimeRouteNode(route RuntimeRoute) *RuntimeRouteNode {
	for index := range route.Nodes {
		if route.Nodes[index].Selected {
			return &route.Nodes[index]
		}
	}
	if route.SelectedMemberID == "" && route.SelectedMemberTag == "" {
		return fallbackRuntimeRouteNode(route)
	}
	for index := range route.Nodes {
		node := &route.Nodes[index]
		if node.NodeID == route.SelectedMemberID || node.NodeTag == route.SelectedMemberTag {
			return node
		}
	}
	return fallbackRuntimeRouteNode(route)
}

func fallbackRuntimeRouteNode(route RuntimeRoute) *RuntimeRouteNode {
	var fallback *RuntimeRouteNode
	for index := range route.Nodes {
		node := &route.Nodes[index]
		if fallback == nil {
			fallback = node
			continue
		}
		if node.LastCheckedAt.After(fallback.LastCheckedAt) {
			fallback = node
			continue
		}
		if fallback.LastCheckedAt.IsZero() && node.Error != "" {
			fallback = node
		}
	}
	return fallback
}

func routeHopFromRuntimeRouteNode(node RuntimeRouteNode) ProxyRouteHopDTO {
	id := node.NodeID
	if node.Kind == ChainMemberTypeGroup {
		id = firstNonEmpty(runtimeRouteGroupID(node.NodeTag), id)
	} else if chainID, groupIndex, memberIndex, ok := parseNodeChainGroupTerminalNodeTag(node.NodeTag); ok {
		id = firstNonEmpty(runtimeChainGroupMemberNodeID(chainID, groupIndex, memberIndex), id)
	}
	return ProxyRouteHopDTO{
		Kind: firstNonEmpty(node.Kind, "node"),
		ID:   id,
		Name: node.NodeName,
		Tag:  node.NodeTag,
	}
}

func testRoutePathForNode(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable) []ProxyRouteHopDTO {
	return testRoutePathForNodeWithGroupSnapshots(ctx, tx, node, nil)
}

func testRoutePathForNodeWithRuntimeRoutes(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, mappingID string, routes []RuntimeRoute) []ProxyRouteHopDTO {
	if len(routes) == 0 || strings.TrimSpace(mappingID) == "" {
		return testRoutePathForNode(ctx, tx, node)
	}
	return testRoutePathForNodeWithGroupSnapshotsAndRuntimeRoutes(ctx, tx, node, nil, mappingID, routes)
}

func testRoutePathForNodeState(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, state singboxcore.CoreState) []ProxyRouteHopDTO {
	groups := make(map[string]singboxcore.GroupSnapshot, len(state.Groups))
	for _, group := range state.Groups {
		if strings.TrimSpace(group.Tag) != "" {
			groups[group.Tag] = group
		}
	}
	return testRoutePathForNodeWithGroupSnapshotsAndRuntimeRoutes(ctx, tx, node, groups, "", nil)
}

func testRoutePathForNodeWithGroupSnapshots(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, groups map[string]singboxcore.GroupSnapshot) []ProxyRouteHopDTO {
	return testRoutePathForNodeWithGroupSnapshotsAndRuntimeRoutes(ctx, tx, node, groups, "", nil)
}

func testRoutePathForNodeWithGroupSnapshotsAndRuntimeRoutes(ctx context.Context, tx model.DBTx, node *tables.ProxyNodeTable, groups map[string]singboxcore.GroupSnapshot, mappingID string, routes []RuntimeRoute) []ProxyRouteHopDTO {
	if node == nil {
		return nil
	}
	if normalizeProtocol(node.Protocol) != ProtocolChain {
		return []ProxyRouteHopDTO{routeHopFromNode(node, nodeOutboundTag(node.ID))}
	}
	members := chainMembersForNode(node)
	if len(members) == 0 {
		return []ProxyRouteHopDTO{routeHopFromNode(node, nodeOutboundTag(node.ID))}
	}
	hops := make([]ProxyRouteHopDTO, 0, len(members))
	for index, member := range members {
		switch normalizeChainMemberType(member.Type) {
		case ChainMemberTypeNode:
			nodes, err := findNodesByIDs(ctx, tx, []string{member.ID})
			if err != nil || len(nodes) != 1 {
				hops = append(hops, ProxyRouteHopDTO{Kind: ChainMemberTypeNode, ID: member.ID, Tag: nodeChainMemberOutboundTag(node.ID, index, member.ID)})
				continue
			}
			hops = append(hops, routeHopFromNode(nodes[0], nodeChainMemberOutboundTag(node.ID, index, member.ID)))
		case ChainMemberTypeGroup:
			foundGroups, err := findGroupsByIDs(ctx, tx, []string{member.ID})
			if err != nil || len(foundGroups) != 1 {
				hops = append(hops, ProxyRouteHopDTO{Kind: ChainMemberTypeGroup, ID: member.ID, Tag: nodeChainMemberGroupOutboundTag(node.ID, index, member.ID)})
			} else {
				hops = append(hops, routeHopFromGroup(foundGroups[0], nodeChainMemberGroupOutboundTag(node.ID, index, member.ID)))
			}
			groupTag := nodeChainMemberGroupOutboundTag(node.ID, index, member.ID)
			if route, ok := runtimeRouteForTag(RuntimeStatus{Routes: routes}, mappingID, groupTag); ok {
				appendSelectedRouteHops(ctx, route, routes, map[string]bool{}, &hops)
				continue
			}
			appendSelectedGroupSnapshotRouteHops(ctx, tx, groupTag, groups, map[string]bool{}, &hops)
		}
	}
	return hops
}

func appendSelectedGroupSnapshotRouteHops(ctx context.Context, tx model.DBTx, groupTag string, groups map[string]singboxcore.GroupSnapshot, visited map[string]bool, hops *[]ProxyRouteHopDTO) {
	if len(groups) == 0 || hops == nil {
		return
	}
	groupTag = strings.TrimSpace(groupTag)
	if groupTag == "" || visited[groupTag] {
		return
	}
	group, ok := groups[groupTag]
	if !ok {
		return
	}
	visited[groupTag] = true

	selected := selectedGroupSnapshotNode(group)
	if selected == nil {
		return
	}
	routeNode := runtimeRouteNodeFromSnapshot(group, *selected, groups)
	hop := routeHopFromRuntimeRouteNode(routeNode)
	hydrateRouteHopName(ctx, tx, &hop)
	*hops = append(*hops, hop)
	if routeNode.Kind == ChainMemberTypeGroup {
		appendSelectedGroupSnapshotRouteHops(ctx, tx, routeNode.NodeTag, groups, visited, hops)
	}
}

func selectedGroupSnapshotNode(group singboxcore.GroupSnapshot) *singboxcore.NodeSnapshot {
	for index := range group.Nodes {
		if group.Nodes[index].ID == group.Selected {
			return &group.Nodes[index]
		}
	}
	var fallback *singboxcore.NodeSnapshot
	for index := range group.Nodes {
		node := &group.Nodes[index]
		if fallback == nil {
			fallback = node
			continue
		}
		if node.LastCheckedAt.After(fallback.LastCheckedAt) {
			fallback = node
			continue
		}
		if fallback.LastCheckedAt.IsZero() && node.LastError != "" {
			fallback = node
		}
	}
	if fallback != nil {
		return fallback
	}
	return nil
}

func hydrateRouteHopName(ctx context.Context, tx model.DBTx, hop *ProxyRouteHopDTO) {
	if hop == nil {
		return
	}
	switch hop.Kind {
	case ChainMemberTypeNode:
		nodes, err := findNodesByIDs(ctx, tx, []string{hop.ID})
		if err == nil && len(nodes) == 1 {
			hop.Name = firstNonEmpty(hop.Name, nodes[0].Name)
		}
	case ChainMemberTypeGroup:
		groupID := firstNonEmpty(runtimeRouteGroupID(hop.Tag), hop.ID)
		if groupID != "" {
			hop.ID = groupID
		}
		groups, err := findGroupsByIDs(ctx, tx, []string{hop.ID})
		if err == nil && len(groups) == 1 {
			hop.Name = firstNonEmpty(hop.Name, groups[0].Name)
		}
	case "builtin":
		hop.Name = firstNonEmpty(hop.Name, hop.Tag, hop.ID)
	}
}

func routeHopFromNode(node *tables.ProxyNodeTable, tag string) ProxyRouteHopDTO {
	if node == nil {
		return ProxyRouteHopDTO{Kind: ChainMemberTypeNode, Tag: tag}
	}
	return ProxyRouteHopDTO{Kind: ChainMemberTypeNode, ID: node.ID, Name: node.Name, Tag: tag}
}

func routeHopFromGroup(group *tables.ProxyGroupTable, tag string) ProxyRouteHopDTO {
	if group == nil {
		return ProxyRouteHopDTO{Kind: ChainMemberTypeGroup, Tag: tag}
	}
	return ProxyRouteHopDTO{Kind: ChainMemberTypeGroup, ID: group.ID, Name: group.Name, Tag: tag}
}

func groupNameMapForRouteHop(ctx context.Context, groupIDs []string) map[string]string {
	groupIDs = uniqueNonEmpty(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}
	groups, err := findGroupsByIDs(ctx, nil, groupIDs)
	if err != nil {
		return nil
	}
	names := make(map[string]string, len(groups))
	for _, group := range groups {
		if group != nil {
			names[group.ID] = group.Name
		}
	}
	return names
}

func runtimeNodeErrorFromProbe(nodeTag string, fallback string) string {
	nodeTag = strings.TrimSpace(nodeTag)
	if nodeTag == "" {
		return ""
	}
	if err := recentSingBoxLogErrorForTag(nodeTag, 3*time.Second); err != "" {
		return err
	}
	return fallback
}

func recentSingBoxLogErrorForTag(tag string, maxAge time.Duration) string {
	logPath := singBoxLogPath()
	info, err := os.Stat(logPath)
	if err != nil || info.IsDir() || time.Since(info.ModTime()) > maxAge {
		return ""
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	marker := "[" + tag + "]"
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, marker) || !strings.Contains(line, "ERROR") {
			continue
		}
		return line
	}
	return ""
}

func singBoxLogPath() string {
	if logPath := strings.TrimSpace(os.Getenv("PROXYHUB_SING_BOX_LOG")); logPath != "" {
		return logPath
	}
	return filepath.Join(utils.GetDataDir(), "sing-box.log")
}

func singBoxLogOutputPath() string {
	logPath := singBoxLogPath()
	dir := strings.TrimSpace(filepath.Dir(logPath))
	if dir == "" || dir == "." {
		return logPath
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		utils.Logger.Warn("创建 sing-box 日志目录失败", zap.String("path", dir), zap.Error(err))
		return ""
	}
	return logPath
}

func runtimeHasInboundForMapping(status RuntimeStatus, mappingID string) bool {
	for _, inbound := range status.Inbounds {
		if inbound.MappingID == mappingID {
			return true
		}
	}
	return false
}

func runtimeFailureForMapping(status RuntimeStatus, mappingID string) *RuntimeInboundFailure {
	for _, failure := range status.Failures {
		if failure.MappingID == mappingID {
			return &failure
		}
	}
	return nil
}

func mappingProbeProxyURL(mapping *tables.PortMappingTable) (*url.URL, error) {
	if mapping == nil {
		return nil, ErrMappingNotFound
	}
	host := strings.TrimSpace(mapping.ListenAddress)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if parsedIP, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && parsedIP.IsUnspecified() {
		if parsedIP.Is6() {
			host = "::1"
		} else {
			host = "127.0.0.1"
		}
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}

	scheme := "http"
	if normalizeOutboundProtocol(mapping.OutboundProtocol) == OutboundProtocolSOCKS {
		scheme = "socks5"
	}
	proxyURL := &url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", host, mapping.ListenPort),
	}
	username := strings.TrimSpace(mapping.Username)
	password := strings.TrimSpace(mapping.Password)
	if username != "" || password != "" {
		proxyURL.User = url.UserPassword(username, password)
	}
	return proxyURL, nil
}

func startHealthProbeProxy(ctx context.Context, node *tables.ProxyNodeTable) (*url.URL, *singboxcore.Core, error) {
	plan, err := buildHealthProbeNodePlan(ctx, node)
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
	rules := []option.Rule{buildInboundRouteRule(inboundTag, plan.tag)}
	options := option.Options{
		Log: &option.LogOptions{
			Level:        "error",
			Output:       singBoxLogOutputPath(),
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
		Outbounds: append(singboxcore.BaseOutbounds(), sortedOutbounds(plan.outbounds)...),
		Route: &option.RouteOptions{
			Rules: rules,
			Final: constant.TypeBlock,
		},
	}
	core, err := singboxcore.NewCore(singboxcore.Config{
		Options: options,
		Context: ctx,
	})
	if err != nil {
		return nil, nil, err
	}
	probeInstance := &runtimeInstance{core: core}
	if _, err := applyDynamicRuntimePlan(ctx, &dynamicRuntimePlan{groups: plan.groups}, probeInstance); err != nil {
		_ = core.Close()
		return nil, nil, err
	}
	if err := core.Start(); err != nil {
		_ = core.Close()
		return nil, nil, err
	}
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", listenPort))
	if err != nil {
		_ = core.Close()
		return nil, nil, err
	}
	return proxyURL, core, nil
}

type healthProbeNodePlan struct {
	tag       string
	outbounds map[string]option.Outbound
	groups    []dynamicGroupPlan
}

func buildHealthProbeNodePlan(ctx context.Context, node *tables.ProxyNodeTable) (*healthProbeNodePlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if node == nil {
		return nil, ErrNodeNotFound
	}
	builder := &dynamicPlanBuilder{
		ctx:                ctx,
		tx:                 model.GetTx(nil).WithContext(ctx),
		outbounds:          map[string]option.Outbound{},
		outboundNodes:      map[string]*tables.ProxyNodeTable{},
		groupPlans:         map[string]*dynamicGroupPlan{},
		blacklistedNodeIDs: map[string]struct{}{},
		excludedNodeIDs:    map[string]struct{}{},
	}
	member, err := builder.memberForNode(node)
	if err != nil {
		return nil, err
	}
	if member.tag == "" {
		return nil, ErrNoAvailableNode
	}
	plan := &healthProbeNodePlan{
		tag:       member.tag,
		outbounds: builder.outbounds,
		groups:    sortedGroupPlans(builder.groupPlans),
	}
	rotateHealthProbeRoundRobinGroups(plan.groups)
	return plan, nil
}

func rotateHealthProbeRoundRobinGroups(groups []dynamicGroupPlan) {
	if len(groups) == 0 {
		return
	}
	healthProbeRoundRobinMu.Lock()
	defer healthProbeRoundRobinMu.Unlock()
	for index := range groups {
		group := &groups[index]
		if group.policy.Strategy != singboxcore.BalanceRoundRobin || len(group.members) <= 1 {
			continue
		}
		offset := healthProbeRoundRobinOffsets[group.tag] % uint64(len(group.members))
		healthProbeRoundRobinOffsets[group.tag]++
		if offset == 0 {
			continue
		}
		rotated := append([]dynamicMemberPlan{}, group.members[offset:]...)
		rotated = append(rotated, group.members[:offset]...)
		group.members = rotated
		group.selected = selectedDynamicMemberID(group.members, group.selected)
	}
}

func buildHealthProbeNodeOutbounds(ctx context.Context, node *tables.ProxyNodeTable) (string, []option.Outbound, error) {
	plan, err := buildHealthProbeNodePlan(ctx, node)
	if err != nil {
		return "", nil, err
	}
	return plan.tag, sortedOutbounds(plan.outbounds), nil
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
	if ctx == nil {
		ctx = context.Background()
	}
	return globalNodeHealthBatcher.get(ctx, nodeID)
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
