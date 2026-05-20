package proxy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/zap"

	"proxy-hub/api/h"
	"proxy-hub/model/tables"
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
		Path:        "/settings/export",
		Summary:     "导出代理设置",
		OperationID: "proxy-settings-export",
		Tags:        []string{proxyTag},
	}, settingsExportHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/settings/export/zip",
		Summary:     "导出代理设置 ZIP",
		OperationID: "proxy-settings-export-zip",
		Tags:        []string{proxyTag},
	}, settingsExportZipHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/settings/import",
		Summary:     "导入代理设置",
		Description: "覆盖恢复节点、节点组、订阅和端口映射配置。",
		OperationID: "proxy-settings-import",
		Tags:        []string{proxyTag},
	}, settingsImportHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/settings/import/zip",
		Summary:     "导入代理设置 ZIP",
		Description: "上传 ZIP 备份并覆盖恢复节点、节点组、订阅和端口映射配置。",
		OperationID: "proxy-settings-import-zip",
		Tags:        []string{proxyTag},
	}, settingsImportZipHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/nodes",
		Summary:     "节点列表",
		OperationID: "proxy-node-list",
		Tags:        []string{proxyTag},
	}, nodeListHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/node-options",
		Summary:     "节点选择项",
		OperationID: "proxy-node-option-list",
		Tags:        []string{proxyTag},
	}, nodeOptionsHandler)

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
		Method:      http.MethodPost,
		Path:        "/nodes/import/preview",
		Summary:     "预览导入节点 URI",
		OperationID: "proxy-node-import-preview",
		Tags:        []string{proxyTag},
	}, nodeImportPreviewHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/nodes/health",
		Summary:     "节点健康状态列表",
		OperationID: "proxy-node-health-list",
		Tags:        []string{proxyTag},
	}, nodeHealthListHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes/probe",
		Summary:     "探测全部节点",
		OperationID: "proxy-node-probe-all",
		Tags:        []string{proxyTag},
	}, nodeProbeAllHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes/{id}/probe",
		Summary:     "探测单个节点",
		OperationID: "proxy-node-probe",
		Tags:        []string{proxyTag},
	}, nodeProbeHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes/{id}/test",
		Summary:     "测试单个节点",
		OperationID: "proxy-node-test",
		Tags:        []string{proxyTag},
	}, nodeTestHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes/{id}/release",
		Summary:     "释放节点黑名单",
		OperationID: "proxy-node-release",
		Tags:        []string{proxyTag},
	}, nodeReleaseHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/nodes/{id}/blacklist",
		Summary:     "手动拉黑节点",
		OperationID: "proxy-node-blacklist",
		Tags:        []string{proxyTag},
	}, nodeBlacklistHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/subscriptions",
		Summary:     "订阅列表",
		OperationID: "proxy-subscription-list",
		Tags:        []string{proxyTag},
	}, subscriptionListHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/subscriptions",
		Summary:     "创建订阅",
		OperationID: "proxy-subscription-create",
		Tags:        []string{proxyTag},
	}, subscriptionCreateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/subscriptions/preview",
		Summary:     "预览订阅导入",
		OperationID: "proxy-subscription-preview",
		Tags:        []string{proxyTag},
	}, subscriptionPreviewHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/subscriptions/{id}",
		Summary:     "更新订阅",
		OperationID: "proxy-subscription-update",
		Tags:        []string{proxyTag},
	}, subscriptionUpdateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodDelete,
		Path:        "/subscriptions/{id}",
		Summary:     "删除订阅",
		OperationID: "proxy-subscription-delete",
		Tags:        []string{proxyTag},
	}, subscriptionDeleteHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/subscriptions/{id}/sync",
		Summary:     "同步订阅",
		OperationID: "proxy-subscription-sync",
		Tags:        []string{proxyTag},
	}, subscriptionSyncHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/groups",
		Summary:     "节点组列表",
		OperationID: "proxy-group-list",
		Tags:        []string{proxyTag},
	}, groupListHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/groups",
		Summary:     "创建节点组",
		OperationID: "proxy-group-create",
		Tags:        []string{proxyTag},
	}, groupCreateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/groups/{id}",
		Summary:     "更新节点组",
		OperationID: "proxy-group-update",
		Tags:        []string{proxyTag},
	}, groupUpdateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodDelete,
		Path:        "/groups/{id}",
		Summary:     "删除节点组",
		OperationID: "proxy-group-delete",
		Tags:        []string{proxyTag},
	}, groupDeleteHandler)

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
		Method:      http.MethodPost,
		Path:        "/mappings/{id}/test",
		Summary:     "测试端口映射",
		OperationID: "proxy-mapping-test",
		Tags:        []string{proxyTag},
	}, mappingTestHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/mappings/{id}/switch",
		Summary:     "切换端口当前线路",
		OperationID: "proxy-mapping-switch",
		Tags:        []string{proxyTag},
	}, mappingSwitchHandler)

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

type settingsExportOutput struct {
	Body proxyService.SettingsBackupDTO `json:"body"`
}

func settingsExportHandler(ctx context.Context, _ *struct{}) (*settingsExportOutput, error) {
	backup, err := proxyService.SettingsExport(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsExportOutput{Body: *backup}, nil
}

type settingsExportZipOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte `json:"body"`
}

func settingsExportZipHandler(ctx context.Context, _ *struct{}) (*settingsExportZipOutput, error) {
	backup, err := proxyService.SettingsExport(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	body, err := proxyService.SettingsBackupToZip(backup)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsExportZipOutput{
		ContentType:        "application/zip",
		ContentDisposition: "attachment; filename=" + settingsBackupZipFileName(backup.ExportedAt),
		Body:               body,
	}, nil
}

type settingsImportInput struct {
	Body proxyService.SettingsBackupDTO
}

type settingsImportOutput struct {
	Body proxyService.SettingsImportResultDTO `json:"body"`
}

func settingsImportHandler(ctx context.Context, input *settingsImportInput) (*settingsImportOutput, error) {
	result, err := proxyService.SettingsImport(ctx, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsImportOutput{Body: *result}, nil
}

type settingsImportZipInput struct {
	RawBody []byte `contentType:"application/zip"`
}

func settingsImportZipHandler(ctx context.Context, input *settingsImportZipInput) (*settingsImportOutput, error) {
	result, err := proxyService.SettingsImportZip(ctx, input.RawBody)
	if err != nil {
		return nil, mapError(err)
	}
	return &settingsImportOutput{Body: *result}, nil
}

func settingsBackupZipFileName(exportedAt time.Time) string {
	if exportedAt.IsZero() {
		exportedAt = time.Now().UTC()
	}
	return "proxyhub-settings-" + exportedAt.UTC().Format("20060102-150405") + ".zip"
}

type nodeListInput struct {
	Page         int      `query:"page" validate:"omitempty,min=1"`
	Size         int      `query:"size" validate:"omitempty,min=1,max=200"`
	Keyword      string   `query:"keyword" validate:"omitempty"`
	NameOnly     bool     `query:"nameOnly"`
	GroupID      string   `query:"groupId" validate:"omitempty"`
	DefaultOnly  bool     `query:"defaultOnly"`
	PhysicalOnly bool     `query:"physicalOnly"`
	WithHealth   bool     `query:"withHealth" default:"true"`
	IDs          []string `query:"ids" explode:"false"`
}

type nodeListOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeDTO `json:"items"`
		Total int64                        `json:"total"`
		Page  int                          `json:"page"`
		Size  int                          `json:"size"`
	} `json:"body"`
}

func nodeListHandler(ctx context.Context, input *nodeListInput) (*nodeListOutput, error) {
	page := utils.GetPage(input.Page)
	size := utils.GetPageSize(input.Size, 50)
	if size > 200 {
		size = 200
	}
	nodes, total, err := proxyService.NodeListPaged(ctx, nil, proxyService.NodeListRequest{
		Keyword:      input.Keyword,
		NameOnly:     input.NameOnly,
		GroupID:      input.GroupID,
		DefaultOnly:  input.DefaultOnly,
		PhysicalOnly: input.PhysicalOnly,
		IDs:          input.IDs,
	}, page, size)
	if err != nil {
		return nil, mapError(err)
	}
	var healthByNodeID map[string]*tables.ProxyNodeHealthTable
	if input.WithHealth {
		healthByNodeID = proxyService.NodeHealthMap(ctx, nil, nodeIDsFromNodes(nodes))
	}
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeListOutput{}
	output.Body.Items = proxyService.ToNodeDTOsWithHealthAndGroups(nodes, healthByNodeID, groups)
	output.Body.Total = total
	output.Body.Page = page
	output.Body.Size = size
	return output, nil
}

type nodeOptionsOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeOptionDTO `json:"items"`
		Total int64                              `json:"total"`
		Page  int                                `json:"page"`
		Size  int                                `json:"size"`
	} `json:"body"`
}

func nodeOptionsHandler(ctx context.Context, input *nodeListInput) (*nodeOptionsOutput, error) {
	page := utils.GetPage(input.Page)
	size := utils.GetPageSize(input.Size, 50)
	if size > 200 {
		size = 200
	}
	nodes, total, err := proxyService.NodeListPaged(ctx, nil, proxyService.NodeListRequest{
		Keyword:      input.Keyword,
		NameOnly:     input.NameOnly,
		GroupID:      input.GroupID,
		DefaultOnly:  input.DefaultOnly,
		PhysicalOnly: input.PhysicalOnly,
		IDs:          input.IDs,
	}, page, size)
	if err != nil {
		return nil, mapError(err)
	}
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeOptionsOutput{}
	output.Body.Items = proxyService.ToNodeOptionDTOs(nodes, groups)
	output.Body.Total = total
	output.Body.Page = page
	output.Body.Size = size
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
	if err := syncRuntimeMappingsForNodes(ctx, []string{node.ID}); err != nil {
		return nil, err
	}
	output := &nodeOutput{}
	output.Body.Item = nodeDTOWithGroups(ctx, node)
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
	affectedBefore, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	node, err := proxyService.NodeUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	affectedAfter, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, []string{node.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(uniqueStrings(append(affectedBefore, affectedAfter...))); err != nil {
		return nil, err
	}
	output := &nodeOutput{}
	output.Body.Item = nodeDTOWithGroups(ctx, node)
	return output, nil
}

func nodeDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	affected, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := proxyService.NodeDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(affected); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("节点已删除"), nil
}

type nodeImportInput struct {
	IncludeItems bool `query:"includeItems" default:"true"`
	Body         proxyService.NodeImportRequest
}

type nodeImportOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func nodeImportHandler(ctx context.Context, input *nodeImportInput) (*nodeImportOutput, error) {
	result, err := proxyService.NodeImport(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if result.Imported > 0 || result.Updated > 0 {
		if err := syncRuntimeMappingsForNodeDTOs(ctx, result.Items); err != nil {
			return nil, err
		}
		groupIDs := make([]string, 0, len(result.Groups))
		for _, group := range result.Groups {
			if group != nil {
				groupIDs = append(groupIDs, group.ID)
			}
		}
		if err := syncRuntimeMappingsForGroups(ctx, groupIDs); err != nil {
			return nil, err
		}
	}
	if !input.IncludeItems {
		result.Items = nil
		result.Groups = nil
	}
	return &nodeImportOutput{Body: *result}, nil
}

func nodeImportPreviewHandler(ctx context.Context, input *nodeImportInput) (*nodeImportOutput, error) {
	result, err := proxyService.NodeImportPreview(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &nodeImportOutput{Body: *result}, nil
}

type nodeHealthListOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeHealthDTO `json:"items"`
	} `json:"body"`
}

func nodeHealthListHandler(ctx context.Context, _ *struct{}) (*nodeHealthListOutput, error) {
	rows, err := proxyService.NodeHealthList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthListOutput{}
	output.Body.Items = proxyService.ToNodeHealthDTOs(rows)
	return output, nil
}

type nodeHealthOutput struct {
	Body struct {
		Item *proxyService.ProxyNodeHealthDTO `json:"item"`
	} `json:"body"`
}

func nodeProbeHandler(ctx context.Context, input *idInput) (*nodeHealthOutput, error) {
	health, err := proxyService.NodeProbe(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthOutput{}
	output.Body.Item = proxyService.ToNodeHealthDTO(health)
	return output, nil
}

type nodeProbeAllOutput struct {
	Body proxyService.NodeHealthProbeAllDTO `json:"body"`
}

func nodeProbeAllHandler(ctx context.Context, _ *struct{}) (*nodeProbeAllOutput, error) {
	result, err := proxyService.NodeProbeAll(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	dto := proxyService.ToNodeHealthProbeAllDTO(result)
	return &nodeProbeAllOutput{Body: *dto}, nil
}

type nodeTestInput struct {
	ID   string `path:"id"`
	Body proxyService.ProxyTestRequest
}

type proxyTestOutput struct {
	Body proxyService.ProxyTestResultDTO `json:"body"`
}

func nodeTestHandler(ctx context.Context, input *nodeTestInput) (*proxyTestOutput, error) {
	result, err := proxyService.NodeTest(ctx, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &proxyTestOutput{Body: *result}, nil
}

func nodeReleaseHandler(ctx context.Context, input *idInput) (*nodeHealthOutput, error) {
	health, err := proxyService.NodeRelease(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthOutput{}
	output.Body.Item = proxyService.ToNodeHealthDTO(health)
	return output, nil
}

type nodeBlacklistInput struct {
	ID   string `path:"id"`
	Body proxyService.NodeBlacklistRequest
}

func nodeBlacklistHandler(ctx context.Context, input *nodeBlacklistInput) (*nodeHealthOutput, error) {
	var duration time.Duration
	if input.Body.Duration != "" {
		parsed, err := time.ParseDuration(input.Body.Duration)
		if err != nil || parsed <= 0 {
			return nil, mapError(proxyService.ErrInvalidHealthDuration)
		}
		duration = parsed
	}
	health, err := proxyService.NodeBlacklist(ctx, input.ID, duration)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthOutput{}
	output.Body.Item = proxyService.ToNodeHealthDTO(health)
	return output, nil
}

type subscriptionListOutput struct {
	Body struct {
		Items []*proxyService.ProxySubscriptionDTO `json:"items"`
	} `json:"body"`
}

func subscriptionListHandler(ctx context.Context, _ *struct{}) (*subscriptionListOutput, error) {
	subscriptions, err := proxyService.SubscriptionList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &subscriptionListOutput{}
	output.Body.Items = proxyService.ToSubscriptionDTOs(subscriptions)
	return output, nil
}

type subscriptionInput struct {
	Body proxyService.SubscriptionUpsertRequest
}

type subscriptionOutput struct {
	Body struct {
		Item *proxyService.ProxySubscriptionDTO `json:"item"`
	} `json:"body"`
}

type subscriptionPreviewInput struct {
	Body proxyService.SubscriptionUpsertRequest
}

type subscriptionPreviewOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func subscriptionPreviewHandler(ctx context.Context, input *subscriptionPreviewInput) (*subscriptionPreviewOutput, error) {
	result, err := proxyService.SubscriptionPreview(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &subscriptionPreviewOutput{Body: *result}, nil
}

func subscriptionCreateHandler(ctx context.Context, input *subscriptionInput) (*subscriptionOutput, error) {
	subscription, err := proxyService.SubscriptionCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	output := &subscriptionOutput{}
	output.Body.Item = proxyService.ToSubscriptionDTO(subscription)
	return output, nil
}

type subscriptionUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.SubscriptionUpsertRequest
}

func subscriptionUpdateHandler(ctx context.Context, input *subscriptionUpdateInput) (*subscriptionOutput, error) {
	subscription, err := proxyService.SubscriptionUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	output := &subscriptionOutput{}
	output.Body.Item = proxyService.ToSubscriptionDTO(subscription)
	return output, nil
}

func subscriptionDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	affected, err := proxyService.RuntimeAffectedMappingIDsBySubscription(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if err := proxyService.SubscriptionDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(affected); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("订阅已删除"), nil
}

type subscriptionSyncInput struct {
	ID           string `path:"id"`
	IncludeItems bool   `query:"includeItems" default:"true"`
	Body         proxyService.SubscriptionSyncRequest
}

type subscriptionSyncOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func subscriptionSyncHandler(ctx context.Context, input *subscriptionSyncInput) (*subscriptionSyncOutput, error) {
	affectedBefore, err := proxyService.RuntimeAffectedMappingIDsBySubscription(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	result, err := proxyService.SubscriptionSync(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	affectedAfter, err := proxyService.RuntimeAffectedMappingIDsBySubscription(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(uniqueStrings(append(affectedBefore, affectedAfter...))); err != nil {
		return nil, err
	}
	if !input.IncludeItems {
		result.Items = nil
		result.Groups = nil
	}
	return &subscriptionSyncOutput{Body: *result}, nil
}

type groupListOutput struct {
	Body struct {
		Items []*proxyService.ProxyGroupDTO `json:"items"`
	} `json:"body"`
}

func groupListHandler(ctx context.Context, _ *struct{}) (*groupListOutput, error) {
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &groupListOutput{}
	output.Body.Items = proxyService.ToGroupDTOs(groups)
	return output, nil
}

type groupInput struct {
	Body proxyService.GroupUpsertRequest
}

type groupOutput struct {
	Body struct {
		Item *proxyService.ProxyGroupDTO `json:"item"`
	} `json:"body"`
}

func groupCreateHandler(ctx context.Context, input *groupInput) (*groupOutput, error) {
	group, err := proxyService.GroupCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappingsForGroups(ctx, []string{group.ID}); err != nil {
		return nil, err
	}
	output := &groupOutput{}
	output.Body.Item = proxyService.ToGroupDTO(group)
	return output, nil
}

type groupUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.GroupUpsertRequest
}

func groupUpdateHandler(ctx context.Context, input *groupUpdateInput) (*groupOutput, error) {
	affectedBefore, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	group, err := proxyService.GroupUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	affectedAfter, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, []string{group.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(uniqueStrings(append(affectedBefore, affectedAfter...))); err != nil {
		return nil, err
	}
	output := &groupOutput{}
	output.Body.Item = proxyService.ToGroupDTO(group)
	return output, nil
}

func groupDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	affected, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := proxyService.GroupDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(affected); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("节点组已删除"), nil
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
	if err := syncRuntimeMapping(ctx, mapping.ID); err != nil {
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
	if err := syncRuntimeMapping(ctx, mapping.ID); err != nil {
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
	if _, err := proxyService.RuntimeRemoveMapping(input.ID); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("端口映射已删除"), nil
}

type mappingTestInput struct {
	ID   string `path:"id"`
	Body proxyService.ProxyTestRequest
}

func mappingTestHandler(ctx context.Context, input *mappingTestInput) (*proxyTestOutput, error) {
	result, err := proxyService.MappingTest(ctx, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &proxyTestOutput{Body: *result}, nil
}

type mappingSwitchInput struct {
	ID   string `path:"id"`
	Body proxyService.MappingSwitchRequest
}

func mappingSwitchHandler(ctx context.Context, input *mappingSwitchInput) (*mappingOutput, error) {
	mapping, err := proxyService.MappingSwitch(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMapping(ctx, mapping.ID); err != nil {
		return nil, err
	}
	output := &mappingOutput{}
	output.Body.Item = proxyService.ToMappingDTO(mapping)
	return output, nil
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

func syncRuntimeMapping(ctx context.Context, mappingID string) error {
	if _, err := proxyService.RuntimeSyncMapping(ctx, mappingID); err != nil {
		utils.Logger.Warn("配置已保存，但代理映射同步失败", zap.String("mappingId", mappingID), zap.Error(err))
	}
	return nil
}

func syncRuntimeMappings(mappingIDs []string) error {
	mappingIDs = uniqueStrings(mappingIDs)
	if len(mappingIDs) == 0 {
		return nil
	}
	if _, err := proxyService.RuntimeSyncMappings(context.Background(), mappingIDs); err != nil {
		utils.Logger.Warn("配置已保存，但代理映射同步失败", zap.Strings("mappingIds", mappingIDs), zap.Error(err))
	}
	return nil
}

func syncRuntimeMappingsForNodes(ctx context.Context, nodeIDs []string) error {
	mappingIDs, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, nodeIDs)
	if err != nil {
		return mapError(err)
	}
	return syncRuntimeMappings(mappingIDs)
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
	return syncRuntimeMappings(mappingIDs)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func mapError(err error) error {
	switch {
	case errors.Is(err, proxyService.ErrNodeNotFound),
		errors.Is(err, proxyService.ErrMappingNotFound),
		errors.Is(err, proxyService.ErrSubscriptionNotFound),
		errors.Is(err, proxyService.ErrGroupNotFound):
		return humanaError(http.StatusNotFound, err.Error())
	case errors.Is(err, proxyService.ErrListenPortTaken):
		return humanaError(http.StatusConflict, "监听端口已存在")
	case errors.Is(err, proxyService.ErrInvalidPort),
		errors.Is(err, proxyService.ErrInvalidAddress),
		errors.Is(err, proxyService.ErrUnsupportedProtocol),
		errors.Is(err, proxyService.ErrUnsupportedURI),
		errors.Is(err, proxyService.ErrNoAvailableNode),
		errors.Is(err, proxyService.ErrUTLSRequired),
		errors.Is(err, proxyService.ErrInvalidSubscription),
		errors.Is(err, proxyService.ErrInvalidGroup),
		errors.Is(err, proxyService.ErrInvalidHealthDuration),
		errors.Is(err, proxyService.ErrInvalidChain),
		errors.Is(err, proxyService.ErrInvalidSettingsBackup),
		errors.Is(err, proxyService.ErrInvalidProbeURL),
		errors.Is(err, proxyService.ErrInvalidMappingSwitch):
		return humanaError(http.StatusBadRequest, err.Error())
	default:
		return humanaError(http.StatusInternalServerError, err.Error())
	}
}

func nodeIDsFromNodes(nodes []*tables.ProxyNodeTable) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func nodeDTOWithGroups(ctx context.Context, node *tables.ProxyNodeTable) *proxyService.ProxyNodeDTO {
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return proxyService.ToNodeDTO(node)
	}
	return proxyService.ToNodeDTOWithGroups(node, groups)
}

func humanaError(code int, message string) error {
	return huma.NewError(code, message)
}
