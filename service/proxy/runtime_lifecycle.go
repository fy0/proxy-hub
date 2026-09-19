package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/option"
	"go.uber.org/zap"

	"proxy-hub/core/singboxcore"
	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/service/proxyuri"
	"proxy-hub/utils"
)

type RuntimeInbound struct {
	MappingID string `json:"mappingId"`
	Tag       string `json:"tag"`
	Listen    string `json:"listen"`
	Outbound  string `json:"outbound"`
}

type RuntimeInboundFailure struct {
	MappingID string `json:"mappingId"`
	Tag       string `json:"tag"`
	Listen    string `json:"listen"`
	Error     string `json:"error"`
}

type RuntimeExcludedNode struct {
	MappingID string `json:"mappingId"`
	NodeID    string `json:"nodeId"`
	NodeName  string `json:"nodeName"`
	Tag       string `json:"tag"`
	Error     string `json:"error"`
}

type RuntimeRouteNode struct {
	NodeID            string    `json:"nodeId"`
	NodeName          string    `json:"nodeName,omitempty"`
	NodeTag           string    `json:"nodeTag"`
	Kind              string    `json:"kind"`
	Selected          bool      `json:"selected"`
	Available         bool      `json:"available"`
	LatencyCandidate  bool      `json:"latencyCandidate"`
	LatencyFallback   bool      `json:"latencyFallback"`
	LatencySlowCount  int       `json:"latencySlowCount"`
	LatencyMs         int64     `json:"latencyMs"`
	Error             string    `json:"error,omitempty"`
	LastCheckedAt     time.Time `json:"lastCheckedAt,omitempty"`
	LastSuccessAt     time.Time `json:"lastSuccessAt,omitempty"`
	ProbeStartedAt    time.Time `json:"probeStartedAt,omitempty"`
	ProbeRunning      bool      `json:"probeRunning"`
	ProbeFailureCount int       `json:"probeFailureCount"`
}

type RuntimeRoute struct {
	MappingID         string             `json:"mappingId"`
	GroupTag          string             `json:"groupTag"`
	Strategy          string             `json:"strategy"`
	SelectedMemberID  string             `json:"selectedMemberId,omitempty"`
	SelectedMemberTag string             `json:"selectedMemberTag,omitempty"`
	SelectedNodeID    string             `json:"selectedNodeId,omitempty"`
	SelectedNodeName  string             `json:"selectedNodeName,omitempty"`
	SelectedNodeTag   string             `json:"selectedNodeTag,omitempty"`
	SelectedNodeKind  string             `json:"selectedNodeKind,omitempty"`
	ProbeRunning      bool               `json:"probeRunning"`
	RuntimeStarted    bool               `json:"runtimeStarted"`
	LastProbeAt       time.Time          `json:"lastProbeAt,omitempty"`
	NextProbeAt       time.Time          `json:"nextProbeAt,omitempty"`
	Nodes             []RuntimeRouteNode `json:"nodes"`
}

type RuntimeStatus struct {
	Running       bool                    `json:"running"`
	State         string                  `json:"state"`
	Error         string                  `json:"error,omitempty"`
	Inbounds      []RuntimeInbound        `json:"inbounds"`
	Failures      []RuntimeInboundFailure `json:"failures"`
	ExcludedNodes []RuntimeExcludedNode   `json:"excludedNodes"`
	Routes        []RuntimeRoute          `json:"routes"`
	UpdatedAt     time.Time               `json:"updatedAt"`
}

type runtimeInstance struct {
	core       *singboxcore.Core
	inboundKey string
	outbounds  map[string]option.Outbound
}

type runtimeManager struct {
	operationMu sync.Mutex
	mu          sync.Mutex
	instances   map[string]*runtimeInstance
	status      RuntimeStatus
}

var singBoxRuntime = &runtimeManager{
	instances: map[string]*runtimeInstance{},
	status: RuntimeStatus{
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	},
}

func RuntimeStatusGet() RuntimeStatus {
	singBoxRuntime.mu.Lock()
	status := singBoxRuntime.status
	status.Inbounds = append([]RuntimeInbound{}, singBoxRuntime.status.Inbounds...)
	status.Failures = append([]RuntimeInboundFailure{}, singBoxRuntime.status.Failures...)
	status.ExcludedNodes = append([]RuntimeExcludedNode{}, singBoxRuntime.status.ExcludedNodes...)
	status.Routes = singBoxRuntime.runtimeRoutesLocked()
	singBoxRuntime.mu.Unlock()

	status.Routes = hydrateRuntimeRouteNames(context.Background(), status.Routes)
	return status
}

func RuntimeReload(ctx context.Context) (RuntimeStatus, error) {
	singBoxRuntime.operationMu.Lock()
	defer singBoxRuntime.operationMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	mappings, err := enabledRuntimeMappings(ctx, nil)
	if err != nil {
		status := setRuntimeError(err)
		return status, err
	}

	oldInstances := singBoxRuntime.replaceRuntimeInstances(RuntimeStatus{
		Running:   false,
		State:     "reloading",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	})

	if closeErr := closeRuntimeInstances(oldInstances); closeErr != nil {
		utils.Logger.Warn("关闭旧 sing-box 实例失败", zap.Error(closeErr))
	}

	instances := make(map[string]*runtimeInstance, len(mappings))
	inbounds := make([]RuntimeInbound, 0, len(mappings))
	failures := make([]RuntimeInboundFailure, 0)
	excludedNodes := make([]RuntimeExcludedNode, 0)

	for _, mapping := range mappings {
		instance, inbound, mappingExcludedNodes, failure := createRuntimeMappingInstance(ctx, mapping)
		excludedNodes = append(excludedNodes, mappingExcludedNodes...)
		if failure != nil {
			failures = append(failures, *failure)
			continue
		}

		instances[mapping.ID] = instance
		inbounds = append(inbounds, inbound)
	}

	singBoxRuntime.setRuntimeInstances(
		instances,
		runtimeStatusFromResults(len(mappings), inbounds, failures, excludedNodes),
	)
	return RuntimeStatusGet(), nil
}

func RuntimeSyncMapping(ctx context.Context, mappingID string) (RuntimeStatus, error) {
	singBoxRuntime.operationMu.Lock()
	defer singBoxRuntime.operationMu.Unlock()
	return runtimeSyncMapping(ctx, mappingID)
}

func runtimeSyncMapping(ctx context.Context, mappingID string) (RuntimeStatus, error) {
	mappingID = strings.TrimSpace(mappingID)
	if mappingID == "" {
		return RuntimeStatusGet(), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RuntimeStatusGet(), err
	}

	mapping, err := MappingGet(ctx, nil, mappingID)
	if errors.Is(err, ErrMappingNotFound) {
		status, removeErr := runtimeRemoveMapping(mappingID)
		return status, removeErr
	}
	if err != nil {
		return RuntimeStatusGet(), err
	}
	if !mapping.Enabled {
		return runtimeRemoveMapping(mapping.ID)
	}

	if updated, status, err := syncRuntimeMappingDynamic(ctx, mapping); updated {
		if err != nil {
			return status, err
		}
		return RuntimeStatusGet(), nil
	}

	oldInstance := singBoxRuntime.detachRuntimeMapping(mapping.ID)
	if closeErr := closeRuntimeInstance(mapping.ID, oldInstance); closeErr != nil {
		utils.Logger.Warn("关闭旧 sing-box 映射实例失败", zap.String("mappingId", mapping.ID), zap.Error(closeErr))
	}

	instance, inbound, excludedNodes, failure := createRuntimeMappingInstance(ctx, mapping)
	if failure != nil {
		singBoxRuntime.setRuntimeMappingFailure(mapping.ID, *failure, excludedNodes)
		return RuntimeStatusGet(), nil
	}
	singBoxRuntime.setRuntimeMappingInstance(mapping.ID, instance, inbound, excludedNodes)
	return RuntimeStatusGet(), nil
}

func RuntimeSyncMappings(ctx context.Context, mappingIDs []string) (RuntimeStatus, error) {
	singBoxRuntime.operationMu.Lock()
	defer singBoxRuntime.operationMu.Unlock()
	mappingIDs = proxyuri.UniqueNonEmpty(mappingIDs)
	if len(mappingIDs) == 0 {
		return RuntimeStatusGet(), nil
	}

	var joined error
	status := RuntimeStatusGet()
	for _, mappingID := range mappingIDs {
		nextStatus, err := runtimeSyncMapping(ctx, mappingID)
		status = nextStatus
		if err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return status, joined
}

func RuntimeRemoveMapping(mappingID string) (RuntimeStatus, error) {
	singBoxRuntime.operationMu.Lock()
	defer singBoxRuntime.operationMu.Unlock()
	return runtimeRemoveMapping(mappingID)
}

func runtimeRemoveMapping(mappingID string) (RuntimeStatus, error) {
	mappingID = strings.TrimSpace(mappingID)
	if mappingID == "" {
		return RuntimeStatusGet(), nil
	}

	oldInstance := singBoxRuntime.detachRuntimeMapping(mappingID)
	err := closeRuntimeInstance(mappingID, oldInstance)
	return RuntimeStatusGet(), err
}

func RuntimeAffectedMappingIDsByNodes(ctx context.Context, nodeIDs []string) ([]string, error) {
	return runtimeAffectedMappingIDsByNodes(ctx, nil, nodeIDs)
}

func RuntimeAffectedMappingIDsByGroups(ctx context.Context, groupIDs []string) ([]string, error) {
	return runtimeAffectedMappingIDsByGroups(ctx, nil, groupIDs)
}

func RuntimeAffectedMappingIDsBySubscription(ctx context.Context, subscriptionID string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return []string{}, nil
	}

	tx := model.GetTx(nil).WithContext(ctx)
	var subscription tables.ProxySubscriptionTable
	if err := tx.First(&subscription, "id = ?", subscriptionID).Error; err != nil {
		return nil, err
	}

	groupIDs := []string{subscription.GroupID}
	var groups []*tables.ProxyGroupTable
	if err := tx.Where("subscription_id = ?", subscriptionID).Find(&groups).Error; err != nil {
		return nil, err
	}
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	return runtimeAffectedMappingIDsByGroups(ctx, tx, groupIDs)
}

func RuntimeStop() error {
	singBoxRuntime.operationMu.Lock()
	defer singBoxRuntime.operationMu.Unlock()
	instances := singBoxRuntime.replaceRuntimeInstances(RuntimeStatus{
		Running:   false,
		State:     "stopped",
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	})

	return closeRuntimeInstances(instances)
}

func syncRuntimeMappingDynamic(ctx context.Context, mapping *tables.PortMappingTable) (bool, RuntimeStatus, error) {
	if mapping == nil {
		return false, RuntimeStatusGet(), nil
	}
	existing := singBoxRuntime.runtimeInstanceForMapping(mapping.ID)
	if existing == nil || existing.core == nil {
		return false, RuntimeStatusGet(), nil
	}
	nextInbound, err := buildMappingInbound(mapping)
	if err != nil {
		return false, RuntimeStatusGet(), err
	}
	nextInboundStatus := RuntimeInbound{
		MappingID: mapping.ID,
		Tag:       nextInbound.Tag,
		Listen:    mappingRuntimeListen(mapping),
		Outbound:  mappingOutboundTag(mapping.ID),
	}
	if existing.inboundKey != runtimeInboundKey(nextInboundStatus, mapping) {
		return false, RuntimeStatusGet(), nil
	}

	excludedNodes, failure, restart := syncRuntimeInstanceMembership(ctx, mapping, existing)
	if restart {
		return false, RuntimeStatusGet(), nil
	}
	if failure != nil {
		if err := closeRuntimeInstance(mapping.ID, existing); err != nil {
			utils.Logger.Warn("关闭失败的 sing-box 映射实例失败", zap.String("mappingId", mapping.ID), zap.Error(err))
		}
		return true, singBoxRuntime.setRuntimeMappingFailure(mapping.ID, *failure, excludedNodes), nil
	}
	return true, singBoxRuntime.setRuntimeMappingInstance(mapping.ID, existing, nextInboundStatus, excludedNodes), nil
}

func enabledRuntimeMappings(ctx context.Context, tx model.DBTx) ([]*tables.PortMappingTable, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tx = model.GetTx(tx).WithContext(ctx)

	var mappings []*tables.PortMappingTable
	if err := tx.Where("enabled = ?", true).Order(mappingOrderClause()).Find(&mappings).Error; err != nil {
		return nil, err
	}
	return mappings, nil
}

type runtimeNodeFailure struct {
	node *tables.ProxyNodeTable
	err  error
}

func excludeRuntimeNode(
	ctx context.Context,
	mapping *tables.PortMappingTable,
	excludedNodeIDs map[string]struct{},
	excludedNodes []RuntimeExcludedNode,
	outboundNodes map[string]*tables.ProxyNodeTable,
	retryNode *runtimeNodeFailure,
) ([]RuntimeExcludedNode, bool) {
	if retryNode == nil || retryNode.node == nil {
		return excludedNodes, false
	}
	if excludedNodeIDs == nil {
		excludedNodeIDs = map[string]struct{}{}
	}
	if _, exists := excludedNodeIDs[retryNode.node.ID]; exists {
		return excludedNodes, false
	}

	excluded := runtimeExcludedNodeFromNode(mapping, retryNode.node, outboundTagForNode(outboundNodes, retryNode.node), retryNode.err)
	excludedNodes = append(excludedNodes, excluded)
	excludedNodeIDs[retryNode.node.ID] = struct{}{}
	if _, err := blacklistRuntimeExcludedNode(ctx, retryNode.node, retryNode.err); err != nil {
		utils.Logger.Warn("自动排除节点写入健康状态失败",
			zap.String("mappingId", mapping.ID),
			zap.String("nodeId", retryNode.node.ID),
			zap.Error(err),
		)
	}
	utils.Logger.Warn("已自动排除运行时不可用节点",
		zap.String("mappingId", mapping.ID),
		zap.String("nodeId", retryNode.node.ID),
		zap.String("nodeName", retryNode.node.Name),
		zap.Error(retryNode.err),
	)
	return excludedNodes, true
}

func createRuntimeMappingInstance(
	ctx context.Context,
	mapping *tables.PortMappingTable,
) (*runtimeInstance, RuntimeInbound, []RuntimeExcludedNode, *RuntimeInboundFailure) {
	excludedNodeIDs := map[string]struct{}{}
	excludedNodes := make([]RuntimeExcludedNode, 0)

	for {
		instance, inbound, outboundNodes, failure, retryNode := createRuntimeMappingInstanceOnce(ctx, mapping, excludedNodeIDs)
		if retryNode == nil {
			return instance, inbound, excludedNodes, failure
		}
		var retry bool
		excludedNodes, retry = excludeRuntimeNode(ctx, mapping, excludedNodeIDs, excludedNodes, outboundNodes, retryNode)
		if !retry {
			if failure == nil {
				nextFailure := runtimeFailureFromMapping(mapping, retryNode.err)
				failure = &nextFailure
			}
			return nil, RuntimeInbound{}, excludedNodes, failure
		}
	}
}

func createRuntimeMappingInstanceOnce(
	ctx context.Context,
	mapping *tables.PortMappingTable,
	excludedNodeIDs map[string]struct{},
) (*runtimeInstance, RuntimeInbound, map[string]*tables.ProxyNodeTable, *RuntimeInboundFailure, *runtimeNodeFailure) {
	plan, err := buildDynamicRuntimePlanForMapping(ctx, nil, mapping, excludedNodeIDs)
	if err != nil {
		if buildErr, ok := asNodeBuildError(err); ok {
			return nil, RuntimeInbound{}, nil, nil, &runtimeNodeFailure{node: buildErr.node, err: buildErr.err}
		}
		failure := runtimeFailureFromMapping(mapping, err)
		return nil, RuntimeInbound{}, nil, &failure, nil
	}
	instance, excludedNodes, failure, retryNode := newRuntimeInstanceFromPlan(ctx, plan)
	if retryNode != nil {
		return nil, RuntimeInbound{}, plan.outboundNodes, nil, retryNode
	}
	if failure != nil {
		if node := nodeFromOutboundInitializeError(errors.New(failure.Error), plan.options.Outbounds, plan.outboundNodes); node != nil {
			return nil, RuntimeInbound{}, plan.outboundNodes, nil, &runtimeNodeFailure{node: node, err: errors.New(failure.Error)}
		}
		_ = excludedNodes
		return nil, RuntimeInbound{}, plan.outboundNodes, failure, nil
	}
	return instance, plan.inbound, plan.outboundNodes, nil, nil
}

func runtimeStatusFromResults(
	total int,
	inbounds []RuntimeInbound,
	failures []RuntimeInboundFailure,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	state := "stopped"
	errorMessage := ""
	switch {
	case total == 0:
		state = "stopped"
	case len(inbounds) > 0 && len(failures) == 0:
		state = "running"
	case len(inbounds) > 0:
		state = "degraded"
	default:
		state = "error"
		errorMessage = "all proxy runtime inbounds failed to start"
	}

	return RuntimeStatus{
		Running:  len(inbounds) > 0,
		State:    state,
		Error:    errorMessage,
		Inbounds: append([]RuntimeInbound(nil), inbounds...),
		Failures: append([]RuntimeInboundFailure(nil), failures...),
		ExcludedNodes: append(
			[]RuntimeExcludedNode(nil),
			excludedNodes...,
		),
		UpdatedAt: time.Now(),
	}
}

func runtimeStatusFromEntries(
	inbounds []RuntimeInbound,
	failures []RuntimeInboundFailure,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	return runtimeStatusFromResults(len(inbounds)+len(failures), inbounds, failures, excludedNodes)
}

func (m *runtimeManager) setRuntimeMappingInstance(
	mappingID string,
	instance *runtimeInstance,
	inbound RuntimeInbound,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.instances == nil {
		m.instances = map[string]*runtimeInstance{}
	}
	m.instances[mappingID] = instance
	inbounds := runtimeInboundsWithoutMapping(m.status.Inbounds, mappingID)
	inbounds = append(inbounds, inbound)
	failures := runtimeFailuresWithoutMapping(m.status.Failures, mappingID)
	allExcludedNodes := runtimeExcludedNodesWithoutMapping(m.status.ExcludedNodes, mappingID)
	allExcludedNodes = append(allExcludedNodes, excludedNodes...)
	status := runtimeStatusFromEntries(inbounds, failures, allExcludedNodes)
	m.status = normalizeRuntimeStatus(status)
	return m.status
}

func (m *runtimeManager) setRuntimeMappingFailure(
	mappingID string,
	failure RuntimeInboundFailure,
	excludedNodes []RuntimeExcludedNode,
) RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.instances == nil {
		m.instances = map[string]*runtimeInstance{}
	}
	delete(m.instances, mappingID)
	inbounds := runtimeInboundsWithoutMapping(m.status.Inbounds, mappingID)
	failures := runtimeFailuresWithoutMapping(m.status.Failures, mappingID)
	failures = append(failures, failure)
	allExcludedNodes := runtimeExcludedNodesWithoutMapping(m.status.ExcludedNodes, mappingID)
	allExcludedNodes = append(allExcludedNodes, excludedNodes...)
	status := runtimeStatusFromEntries(inbounds, failures, allExcludedNodes)
	m.status = normalizeRuntimeStatus(status)
	return m.status
}

func (m *runtimeManager) detachRuntimeMapping(mappingID string) *runtimeInstance {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.instances == nil {
		m.instances = map[string]*runtimeInstance{}
	}
	instance := m.instances[mappingID]
	delete(m.instances, mappingID)
	inbounds := runtimeInboundsWithoutMapping(m.status.Inbounds, mappingID)
	failures := runtimeFailuresWithoutMapping(m.status.Failures, mappingID)
	excludedNodes := runtimeExcludedNodesWithoutMapping(m.status.ExcludedNodes, mappingID)
	m.status = normalizeRuntimeStatus(runtimeStatusFromEntries(inbounds, failures, excludedNodes))
	return instance
}

func (m *runtimeManager) runtimeInstanceForMapping(mappingID string) *runtimeInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.instances == nil {
		return nil
	}
	return m.instances[strings.TrimSpace(mappingID)]
}

func (m *runtimeManager) replaceRuntimeInstances(status RuntimeStatus) map[string]*runtimeInstance {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.instances
	m.instances = map[string]*runtimeInstance{}
	m.status = normalizeRuntimeStatus(status)
	return old
}

func (m *runtimeManager) setRuntimeInstances(instances map[string]*runtimeInstance, status RuntimeStatus) RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	if instances == nil {
		instances = map[string]*runtimeInstance{}
	}
	status = normalizeRuntimeStatus(status)
	m.instances = instances
	m.status = status
	return status
}

func (m *runtimeManager) setRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	status = normalizeRuntimeStatus(status)
	m.status = status
	return status
}

func normalizeRuntimeStatus(status RuntimeStatus) RuntimeStatus {
	if status.Inbounds == nil {
		status.Inbounds = []RuntimeInbound{}
	}
	if status.Failures == nil {
		status.Failures = []RuntimeInboundFailure{}
	}
	if status.ExcludedNodes == nil {
		status.ExcludedNodes = []RuntimeExcludedNode{}
	}
	if status.Routes == nil {
		status.Routes = []RuntimeRoute{}
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now()
	}
	return status
}

func setRuntimeError(err error) RuntimeStatus {
	status := RuntimeStatus{
		Running:   false,
		State:     "error",
		Error:     err.Error(),
		Inbounds:  []RuntimeInbound{},
		Failures:  []RuntimeInboundFailure{},
		UpdatedAt: time.Now(),
	}
	return singBoxRuntime.setRuntimeStatus(status)
}

func closeRuntimeInstances(instances map[string]*runtimeInstance) error {
	errs := make([]error, 0)
	for id, instance := range instances {
		if err := closeRuntimeInstance(id, instance); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func closeRuntimeInstance(id string, instance *runtimeInstance) error {
	if instance == nil {
		return nil
	}
	if err := instance.core.Close(); err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	return nil
}
