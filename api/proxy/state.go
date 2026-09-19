package proxy

import (
	"context"

	"go.uber.org/zap"

	proxyService "proxy-hub/service/proxy"
	"proxy-hub/service/proxyuri"
	"proxy-hub/utils"
)

type stateOutput struct {
	Body proxyService.StateSnapshotDTO `json:"body"`
}

type stateInput struct {
	IncludeNodes        bool `query:"includeNodes" default:"true"`
	IncludeGroupMembers bool `query:"includeGroupMembers" default:"true"`
}

func stateHandler(ctx context.Context, input *stateInput) (*stateOutput, error) {
	snapshot, err := proxyService.StateSnapshot(ctx, nil, proxyService.StateSnapshotOptions{
		IncludeNodes:        input.IncludeNodes,
		IncludeGroupMembers: input.IncludeGroupMembers,
	})
	if err != nil {
		return nil, mapError(err)
	}
	return &stateOutput{Body: *snapshot}, nil
}

type runtimeStatusOutput struct {
	Body proxyService.RuntimeStatus `json:"body"`
}

func runtimeStatusHandler(context.Context, *struct{}) (*runtimeStatusOutput, error) {
	return &runtimeStatusOutput{Body: proxyService.RuntimeStatusGet()}, nil
}

func runtimeReloadHandler(context.Context, *struct{}) (*runtimeStatusOutput, error) {
	status, err := proxyService.RuntimeReload(context.Background())
	if err != nil {
		return nil, mapError(err)
	}
	return &runtimeStatusOutput{Body: status}, nil
}

func syncRuntimeMapping(ctx context.Context, mappingID string) {
	if _, err := proxyService.RuntimeSyncMapping(ctx, mappingID); err != nil {
		utils.Logger.Warn("配置已保存，但代理映射同步失败", zap.String("mappingId", mappingID), zap.Error(err))
	}
}

func syncRuntimeMappings(mappingIDs []string) {
	mappingIDs = proxyuri.UniqueNonEmpty(mappingIDs)
	if len(mappingIDs) == 0 {
		return
	}
	if _, err := proxyService.RuntimeSyncMappings(context.Background(), mappingIDs); err != nil {
		utils.Logger.Warn("配置已保存，但代理映射同步失败", zap.Strings("mappingIds", mappingIDs), zap.Error(err))
	}
}

func syncRuntimeMappingsForNodes(ctx context.Context, nodeIDs []string) error {
	mappingIDs, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, nodeIDs)
	if err != nil {
		return mapError(err)
	}
	syncRuntimeMappings(mappingIDs)
	return nil
}

func syncRuntimeMappingsForNodeDTOs(ctx context.Context, nodes []*proxyService.ProxyNodeDTO) error {
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			nodeIDs = append(nodeIDs, node.ID)
		}
	}
	return syncRuntimeMappingsForNodes(ctx, nodeIDs)
}

func syncRuntimeMappingsForGroups(ctx context.Context, groupIDs []string) error {
	mappingIDs, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, groupIDs)
	if err != nil {
		return mapError(err)
	}
	syncRuntimeMappings(mappingIDs)
	return nil
}
