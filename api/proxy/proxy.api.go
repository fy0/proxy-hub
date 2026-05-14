package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/zap"

	"proxy-hub/api/h"
	proxyService "proxy-hub/service/proxy"
	"proxy-hub/utils"
)

const (
	proxyTag  = "proxy-代理"
	proxyPath = "/proxy"
)

func Register(api huma.API) {
	group := huma.NewGroup(api, proxyPath)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/state",
		Summary:     "代理配置快照",
		OperationID: "proxy-state",
		Tags:        []string{proxyTag},
	}, stateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/nodes",
		Summary:     "节点列表",
		OperationID: "proxy-node-list",
		Tags:        []string{proxyTag},
	}, nodeListHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes",
		Summary:     "创建节点",
		OperationID: "proxy-node-create",
		Tags:        []string{proxyTag},
	}, nodeCreateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/nodes/{id}",
		Summary:     "更新节点",
		OperationID: "proxy-node-update",
		Tags:        []string{proxyTag},
	}, nodeUpdateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodDelete,
		Path:        "/nodes/{id}",
		Summary:     "删除节点",
		OperationID: "proxy-node-delete",
		Tags:        []string{proxyTag},
	}, nodeDeleteHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes/import",
		Summary:     "导入节点 URI",
		OperationID: "proxy-node-import",
		Tags:        []string{proxyTag},
	}, nodeImportHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/mappings",
		Summary:     "端口映射列表",
		OperationID: "proxy-mapping-list",
		Tags:        []string{proxyTag},
	}, mappingListHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/mappings",
		Summary:     "创建端口映射",
		OperationID: "proxy-mapping-create",
		Tags:        []string{proxyTag},
	}, mappingCreateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/mappings/{id}",
		Summary:     "更新端口映射",
		OperationID: "proxy-mapping-update",
		Tags:        []string{proxyTag},
	}, mappingUpdateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodDelete,
		Path:        "/mappings/{id}",
		Summary:     "删除端口映射",
		OperationID: "proxy-mapping-delete",
		Tags:        []string{proxyTag},
	}, mappingDeleteHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/runtime/status",
		Summary:     "代理运行状态",
		OperationID: "proxy-runtime-status",
		Tags:        []string{proxyTag},
	}, runtimeStatusHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/runtime/reload",
		Summary:     "重载代理运行时",
		OperationID: "proxy-runtime-reload",
		Tags:        []string{proxyTag},
	}, runtimeReloadHandler)
}

type stateOutput struct {
	Body proxyService.StateSnapshotDTO `json:"body"`
}

func stateHandler(ctx context.Context, _ *struct{}) (*stateOutput, error) {
	snapshot, err := proxyService.StateSnapshot(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	return &stateOutput{Body: *snapshot}, nil
}

type nodeListOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeDTO `json:"items"`
	} `json:"body"`
}

func nodeListHandler(ctx context.Context, _ *struct{}) (*nodeListOutput, error) {
	nodes, err := proxyService.NodeList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeListOutput{}
	output.Body.Items = proxyService.ToNodeDTOs(nodes)
	return output, nil
}

type nodeInput struct {
	Body proxyService.NodeUpsertRequest
}

type nodeOutput struct {
	Body struct {
		Item *proxyService.ProxyNodeDTO `json:"item"`
	} `json:"body"`
}

func nodeCreateHandler(ctx context.Context, input *nodeInput) (*nodeOutput, error) {
	node, err := proxyService.NodeCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := reloadRuntimeAfterMutation(); err != nil {
		return nil, err
	}
	output := &nodeOutput{}
	output.Body.Item = proxyService.ToNodeDTO(node)
	return output, nil
}

type idInput struct {
	ID string `path:"id"`
}

type nodeUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.NodeUpsertRequest
}

func nodeUpdateHandler(ctx context.Context, input *nodeUpdateInput) (*nodeOutput, error) {
	node, err := proxyService.NodeUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := reloadRuntimeAfterMutation(); err != nil {
		return nil, err
	}
	output := &nodeOutput{}
	output.Body.Item = proxyService.ToNodeDTO(node)
	return output, nil
}

func nodeDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	if err := proxyService.NodeDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := reloadRuntimeAfterMutation(); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("节点已删除"), nil
}

type nodeImportInput struct {
	Body proxyService.NodeImportRequest
}

type nodeImportOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func nodeImportHandler(ctx context.Context, input *nodeImportInput) (*nodeImportOutput, error) {
	result, err := proxyService.NodeImport(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if result.Imported > 0 {
		if err := reloadRuntimeAfterMutation(); err != nil {
			return nil, err
		}
	}
	return &nodeImportOutput{Body: *result}, nil
}

type mappingListOutput struct {
	Body struct {
		Items []*proxyService.PortMappingDTO `json:"items"`
	} `json:"body"`
}

func mappingListHandler(ctx context.Context, _ *struct{}) (*mappingListOutput, error) {
	mappings, err := proxyService.MappingList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &mappingListOutput{}
	output.Body.Items = proxyService.ToMappingDTOs(mappings)
	return output, nil
}

type mappingInput struct {
	Body proxyService.MappingUpsertRequest
}

type mappingOutput struct {
	Body struct {
		Item *proxyService.PortMappingDTO `json:"item"`
	} `json:"body"`
}

func mappingCreateHandler(ctx context.Context, input *mappingInput) (*mappingOutput, error) {
	mapping, err := proxyService.MappingCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := reloadRuntimeAfterMutation(); err != nil {
		return nil, err
	}
	output := &mappingOutput{}
	output.Body.Item = proxyService.ToMappingDTO(mapping)
	return output, nil
}

type mappingUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.MappingUpsertRequest
}

func mappingUpdateHandler(ctx context.Context, input *mappingUpdateInput) (*mappingOutput, error) {
	mapping, err := proxyService.MappingUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := reloadRuntimeAfterMutation(); err != nil {
		return nil, err
	}
	output := &mappingOutput{}
	output.Body.Item = proxyService.ToMappingDTO(mapping)
	return output, nil
}

func mappingDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	if err := proxyService.MappingDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := reloadRuntimeAfterMutation(); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("端口映射已删除"), nil
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

func reloadRuntimeAfterMutation() error {
	if _, err := proxyService.RuntimeReload(context.Background()); err != nil {
		utils.Logger.Warn("配置已保存，但代理重载失败", zap.Error(err))
	}
	return nil
}

func mapError(err error) error {
	switch {
	case errors.Is(err, proxyService.ErrNodeNotFound), errors.Is(err, proxyService.ErrMappingNotFound):
		return humanaError(http.StatusNotFound, err.Error())
	case errors.Is(err, proxyService.ErrListenPortTaken):
		return humanaError(http.StatusConflict, "监听端口已存在")
	case errors.Is(err, proxyService.ErrInvalidPort),
		errors.Is(err, proxyService.ErrInvalidAddress),
		errors.Is(err, proxyService.ErrUnsupportedProtocol),
		errors.Is(err, proxyService.ErrUnsupportedURI),
		errors.Is(err, proxyService.ErrNoAvailableNode),
		errors.Is(err, proxyService.ErrUTLSRequired):
		return humanaError(http.StatusBadRequest, err.Error())
	default:
		return humanaError(http.StatusInternalServerError, err.Error())
	}
}

func humanaError(code int, message string) error {
	return huma.NewError(code, message)
}
