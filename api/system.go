package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"proxy-hub/api/h"
	"proxy-hub/utils"
)

const systemTag = "system-系统"

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

func registerSystemRoutes(api huma.API) {
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
