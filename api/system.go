package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"proxy-hub/api/h"
	"proxy-hub/utils"
)

const (
	systemTag            = "system-系统"
	defaultSystemServeAt = ":3020"
)

type systemRouteOptions struct {
	Config         *utils.AppConfig
	RunningServeAt string
	RequestRestart func() error
}

type versionResponse struct {
	Body struct {
		Name    string `json:"name" doc:"应用名称"`
		Version string `json:"version" doc:"版本号"`
		Channel string `json:"channel" doc:"更新频道"`
	} `json:"body"`
}

type checkUpdateResponse struct {
	Body struct {
		CurrentVersion string `json:"currentVersion" doc:"当前版本"`
		LatestVersion  string `json:"latestVersion" doc:"最新版本"`
		HasUpdate      bool   `json:"hasUpdate" doc:"是否有更新"`
		Channel        string `json:"channel" doc:"更新频道"`
		DistTag        string `json:"distTag" doc:"npm dist-tag"`
		UpdateURL      string `json:"updateUrl,omitempty" doc:"更新地址"`
		UpdateCommand  string `json:"updateCommand,omitempty" doc:"更新命令"`
		Message        string `json:"message,omitempty" doc:"提示信息"`
	} `json:"body"`
}

type listenConfigBody struct {
	ServeAt         string `json:"serveAt" doc:"保存的服务监听地址"`
	RunningServeAt  string `json:"runningServeAt" doc:"当前运行中的服务监听地址"`
	ListenAddress   string `json:"listenAddress" doc:"监听 IP 或 localhost，空字符串表示所有地址"`
	ListenPort      int    `json:"listenPort" doc:"监听端口"`
	RestartRequired bool   `json:"restartRequired" doc:"是否需要重启后生效"`
}

type listenConfigResponse struct {
	Body listenConfigBody `json:"body"`
}

type listenConfigUpdateRequest struct {
	ListenAddress string `json:"listenAddress" doc:"监听 IP 或 localhost，空字符串表示所有地址"`
	ListenPort    int    `json:"listenPort" doc:"监听端口"`
}

type listenConfigUpdateInput struct {
	Body listenConfigUpdateRequest
}

type listenConfigUpdateResponse struct {
	Body struct {
		Message string           `json:"message" doc:"提示信息"`
		Item    listenConfigBody `json:"item" doc:"保存后的服务监听配置"`
	} `json:"body"`
}

type restartRequest struct {
	Confirm bool `json:"confirm" doc:"必须为 true 才会执行重启"`
}

type restartInput struct {
	Body restartRequest
}

type restartResponse struct {
	Status int `json:"-"`
	Body   struct {
		Message string `json:"message" doc:"提示信息"`
	} `json:"body"`
}

func registerSystemRoutes(api huma.API, routeOptions ...systemRouteOptions) {
	options := normalizeSystemRouteOptions(routeOptions...)

	h.HumaRegister(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/system/version",
		Summary:     "获取应用版本信息",
		OperationID: "system-version",
		Tags:        []string{systemTag},
	}, systemVersionHandler)

	h.HumaRegister(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/system/check-update",
		Summary:     "检查版本更新",
		Description: "使用 npm registry dist-tag 低频检查版本更新，并返回 GitHub Release 链接",
		OperationID: "system-check-update",
		Tags:        []string{systemTag},
	}, systemCheckUpdateHandler)

	h.HumaRegister(api, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/system/listen",
		Summary:     "获取服务监听配置",
		OperationID: "system-listen-get",
		Tags:        []string{systemTag},
	}, func(ctx context.Context, input *struct{}) (*listenConfigResponse, error) {
		return systemListenGetHandler(ctx, input, options)
	})

	h.HumaRegister(api, huma.Operation{
		Method:      http.MethodPut,
		Path:        "/system/listen",
		Summary:     "更新服务监听配置",
		Description: "写入 serveAt 配置，需重启服务后生效。",
		OperationID: "system-listen-update",
		Tags:        []string{systemTag},
	}, func(ctx context.Context, input *listenConfigUpdateInput) (*listenConfigUpdateResponse, error) {
		return systemListenUpdateHandler(ctx, input, options)
	})

	h.HumaRegister(api, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/system/restart",
		Summary:     "重启服务",
		Description: "确认后触发内部重启，重新读取配置并启动 HTTP 服务。",
		OperationID: "system-restart",
		Tags:        []string{systemTag},
	}, func(ctx context.Context, input *restartInput) (*restartResponse, error) {
		return systemRestartHandler(ctx, input, options)
	})
}

func systemVersionHandler(ctx context.Context, _ *struct{}) (*versionResponse, error) {
	_ = ctx
	info := currentAppInfo()

	resp := &versionResponse{}
	resp.Body.Name = info.Name
	resp.Body.Version = info.Version
	resp.Body.Channel = info.Channel
	return resp, nil
}

func systemCheckUpdateHandler(ctx context.Context, _ *struct{}) (*checkUpdateResponse, error) {
	_ = ctx
	info := currentAppInfo()

	resp := &checkUpdateResponse{}
	resp.Body.CurrentVersion = info.Version

	checker := utils.NewVersionCheckerWithChannel(info.Version, info.PackageName, info.Channel)
	update, err := checker.CheckUpdateInfoCached()
	if err != nil {
		resp.Body.LatestVersion = info.Version
		resp.Body.HasUpdate = false
		resp.Body.Channel = info.Channel
		resp.Body.Message = "无法检查更新: " + err.Error()
		return resp, nil
	}

	resp.Body.LatestVersion = update.LatestVersion
	resp.Body.HasUpdate = update.HasUpdate
	resp.Body.Channel = update.Channel
	resp.Body.DistTag = update.DistTag
	resp.Body.UpdateURL = update.UpdateURL
	resp.Body.UpdateCommand = update.UpdateCommand
	if update.HasUpdate {
		if update.UpdateCommand != "" {
			resp.Body.Message = "发现新版本，请使用 " + update.UpdateCommand + " 更新"
		} else {
			resp.Body.Message = "发现新版本，请前往 GitHub Releases 下载"
		}
	} else {
		resp.Body.Message = "当前已是最新版本"
	}
	return resp, nil
}

func systemListenGetHandler(ctx context.Context, _ *struct{}, options systemRouteOptions) (*listenConfigResponse, error) {
	_ = ctx
	body, err := listenConfigFromServeAt(options.currentServeAt(), options.runningServeAt())
	if err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, err.Error())
	}
	return &listenConfigResponse{Body: body}, nil
}

func systemListenUpdateHandler(ctx context.Context, input *listenConfigUpdateInput, options systemRouteOptions) (*listenConfigUpdateResponse, error) {
	_ = ctx
	if input == nil {
		return nil, huma.NewError(http.StatusBadRequest, "请求体不能为空")
	}

	serveAt, err := buildListenServeAt(input.Body.ListenAddress, input.Body.ListenPort)
	if err != nil {
		return nil, huma.NewError(http.StatusBadRequest, err.Error())
	}

	updated, err := utils.UpdateConfig(func(config *utils.AppConfig) error {
		config.ServeAt = serveAt
		return nil
	})
	if err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, err.Error())
	}
	if options.Config != nil {
		options.Config.ServeAt = updated.ServeAt
	}

	body, err := listenConfigFromServeAt(updated.ServeAt, options.runningServeAt())
	if err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, err.Error())
	}

	resp := &listenConfigUpdateResponse{}
	resp.Body.Message = "服务监听配置已保存，重启后生效"
	resp.Body.Item = body
	return resp, nil
}

func systemRestartHandler(ctx context.Context, input *restartInput, options systemRouteOptions) (*restartResponse, error) {
	_ = ctx
	if input == nil || !input.Body.Confirm {
		return nil, huma.NewError(http.StatusBadRequest, "请先确认重启服务")
	}
	if options.RequestRestart == nil {
		return nil, huma.NewError(http.StatusServiceUnavailable, "当前运行模式不支持内部重启")
	}
	if err := options.RequestRestart(); err != nil {
		return nil, huma.NewError(http.StatusInternalServerError, err.Error())
	}

	resp := &restartResponse{Status: http.StatusAccepted}
	resp.Body.Message = "服务正在重启"
	return resp, nil
}

func normalizeSystemRouteOptions(routeOptions ...systemRouteOptions) systemRouteOptions {
	options := systemRouteOptions{}
	if len(routeOptions) > 0 {
		options = routeOptions[0]
	}
	if options.Config == nil {
		options.Config = &utils.AppConfig{ServeAt: defaultSystemServeAt}
	}
	if strings.TrimSpace(options.Config.ServeAt) == "" {
		options.Config.ServeAt = defaultSystemServeAt
	}
	if strings.TrimSpace(options.RunningServeAt) == "" {
		options.RunningServeAt = options.Config.ServeAt
	}
	return options
}

func (options systemRouteOptions) currentServeAt() string {
	if options.Config == nil || strings.TrimSpace(options.Config.ServeAt) == "" {
		return defaultSystemServeAt
	}
	return options.Config.ServeAt
}

func (options systemRouteOptions) runningServeAt() string {
	if strings.TrimSpace(options.RunningServeAt) == "" {
		return options.currentServeAt()
	}
	return options.RunningServeAt
}

func listenConfigFromServeAt(serveAt, runningServeAt string) (listenConfigBody, error) {
	address, port, normalizedServeAt, err := parseListenServeAt(serveAt)
	if err != nil {
		return listenConfigBody{}, err
	}
	_, _, normalizedRunningServeAt, err := parseListenServeAt(runningServeAt)
	if err != nil {
		normalizedRunningServeAt = strings.TrimSpace(runningServeAt)
	}

	return listenConfigBody{
		ServeAt:         normalizedServeAt,
		RunningServeAt:  normalizedRunningServeAt,
		ListenAddress:   address,
		ListenPort:      port,
		RestartRequired: normalizedServeAt != normalizedRunningServeAt,
	}, nil
}

func parseListenServeAt(serveAt string) (string, int, string, error) {
	serveAt = strings.TrimSpace(serveAt)
	if serveAt == "" {
		serveAt = defaultSystemServeAt
	}

	host, portValue, err := net.SplitHostPort(serveAt)
	if err != nil {
		return "", 0, "", fmt.Errorf("服务监听地址格式无效: %s", serveAt)
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return "", 0, "", fmt.Errorf("监听端口必须是数字")
	}
	normalized, err := buildListenServeAt(host, port)
	if err != nil {
		return "", 0, "", err
	}
	return strings.TrimSpace(host), port, normalized, nil
}

func buildListenServeAt(address string, port int) (string, error) {
	address = strings.TrimSpace(address)
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("监听端口必须在 1 到 65535 之间")
	}
	if err := validateListenAddress(address); err != nil {
		return "", err
	}
	return net.JoinHostPort(address, strconv.Itoa(port)), nil
}

func validateListenAddress(address string) error {
	if address == "" || strings.EqualFold(address, "localhost") {
		return nil
	}
	if _, err := netip.ParseAddr(address); err == nil {
		return nil
	}
	return fmt.Errorf("监听地址必须为空、localhost 或有效 IP 地址")
}

func currentAppInfo() AppInfo {
	if appInfo == nil {
		return AppInfo{
			Name:        "ProxyHub",
			Version:     "0.0.0",
			Channel:     "unknown",
			PackageName: "pxhub",
		}
	}
	return *appInfo
}
