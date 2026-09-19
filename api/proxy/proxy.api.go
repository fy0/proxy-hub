package proxy

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"proxy-hub/api/h"
	proxyService "proxy-hub/service/proxy"
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
		Path:        "/mappings/{id}/ip-lookup",
		Summary:     "查询端口出口 IP",
		OperationID: "proxy-mapping-ip-lookup",
		Tags:        []string{proxyTag},
	}, mappingIPLookupHandler)

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
		errors.Is(err, proxyService.ErrInvalidMapping),
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

func humanaError(code int, message string) error {
	return huma.NewError(code, message)
}
