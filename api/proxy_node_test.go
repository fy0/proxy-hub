package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm/logger"

	"proxy-hub/api/h"
	proxyAPI "proxy-hub/api/proxy"
	"proxy-hub/model"
	proxyService "proxy-hub/service/proxy"
	"proxy-hub/utils"
)

func TestProxyNodeCreateAcceptsRawVLESSWithoutManualFields(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	h.HumaTypesRegister()
	h.HumaValidatePatch()
	proxyAPI.Register(v1)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	payload := map[string]any{
		"rawUri": "vless://48a25c54-8826-4657-330e-8db38ef76716@us-n1.qq.org:6515?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.learn.microsoft.com&fp=chrome&pbk=j0WAnZjnHwzpiPwpHaurvyfqe1yZdbNeRG0isinebQc&spx=%2F&type=tcp&headerType=none#%E7%BE%8E%E8%A5%BFSJ_CN2",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "/api/v1/proxy/nodes", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}

	var response struct {
		Item struct {
			Name     string  `json:"name"`
			Protocol string  `json:"protocol"`
			Server   string  `json:"server"`
			Port     *uint16 `json:"port"`
		} `json:"item"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Item.Name != "美西SJ_CN2" {
		t.Fatalf("Name = %q, want decoded URI fragment", response.Item.Name)
	}
	if response.Item.Protocol != "vless" {
		t.Fatalf("Protocol = %q, want vless", response.Item.Protocol)
	}
	if response.Item.Server != "us-n1.qq.org" {
		t.Fatalf("Server = %q, want us-n1.qq.org", response.Item.Server)
	}
	if response.Item.Port == nil || *response.Item.Port != 6515 {
		t.Fatalf("Port = %v, want 6515", response.Item.Port)
	}
}

func TestProxyNodeCreateReturnsMappedBusinessError(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	h.HumaTypesRegister()
	h.HumaValidatePatch()
	proxyAPI.Register(v1)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"/api/v1/proxy/nodes",
		strings.NewReader(`{"rawUri":"nope"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})

	if got := resp.StatusCode; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
	}

	var response struct {
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Detail != "unsupported proxy uri: nope" {
		t.Fatalf("detail = %q, want unsupported proxy uri", response.Detail)
	}
}

func TestProxyNodeImportPreviewDoesNotPersistClashItems(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	h.HumaTypesRegister()
	h.HumaValidatePatch()
	proxyAPI.Register(v1)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	raw := `proxies:
  - name: hk
    type: trojan
    server: hk.example.com
    port: 443
    password: secret
proxy-groups:
  - name: all
    type: select
    proxies:
      - hk
`
	body, err := json.Marshal(map[string]any{"raw": raw})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "/api/v1/proxy/nodes/import/preview", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})
	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}

	var response struct {
		PreviewItems []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		} `json:"previewItems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.PreviewItems) == 0 {
		t.Fatalf("preview items empty")
	}
	nodes, err := proxyService.NodeList(t.Context(), nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	groups, err := proxyService.GroupList(t.Context(), nil)
	if err != nil {
		t.Fatalf("GroupList() error = %v", err)
	}
	if len(nodes) != 0 || len(groups) != 0 {
		t.Fatalf("preview persisted nodes=%d groups=%d, want none", len(nodes), len(groups))
	}
}

func TestProxySubscriptionPreviewDoesNotPersistSubscription(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	subscriptionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(`proxies:
  - name: hk
    type: trojan
    server: hk.example.com
    port: 443
    password: secret
proxy-groups:
  - name: ruleset-target
    type: select
    proxies:
      - hk
rules:
  - RULE-SET,private,ruleset-target
`))
	}))
	t.Cleanup(subscriptionServer.Close)

	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	h.HumaTypesRegister()
	h.HumaValidatePatch()
	proxyAPI.Register(v1)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	body, err := json.Marshal(map[string]any{"name": "remote", "url": subscriptionServer.URL})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "/api/v1/proxy/subscriptions/preview", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})
	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}
	var response struct {
		PreviewItems []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
			Reason string `json:"reason"`
		} `json:"previewItems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	foundRulesetSkip := false
	for _, item := range response.PreviewItems {
		if item.Name == "ruleset-target" && item.Action == "skip" && item.Reason == "ruleset-policy-group" {
			foundRulesetSkip = true
			break
		}
	}
	if !foundRulesetSkip {
		t.Fatalf("preview items = %+v, want ruleset skip", response.PreviewItems)
	}
	subscriptions, err := proxyService.SubscriptionList(t.Context(), nil)
	if err != nil {
		t.Fatalf("SubscriptionList() error = %v", err)
	}
	nodes, err := proxyService.NodeList(t.Context(), nil)
	if err != nil {
		t.Fatalf("NodeList() error = %v", err)
	}
	if len(subscriptions) != 0 || len(nodes) != 0 {
		t.Fatalf("preview persisted subscriptions=%d nodes=%d, want none", len(subscriptions), len(nodes))
	}
}

func TestProxyNodeHealthBlacklistAndRelease(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	h.HumaTypesRegister()
	h.HumaValidatePatch()
	proxyAPI.Register(v1)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	port := uint16(1080)
	node, err := proxyService.NodeCreate(t.Context(), nil, proxyService.NodeUpsertRequest{
		Name:     "health",
		Protocol: proxyService.ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}

	blacklistReq, err := http.NewRequest(
		http.MethodPost,
		"/api/v1/proxy/nodes/"+node.ID+"/blacklist",
		strings.NewReader(`{"duration":"30m"}`),
	)
	if err != nil {
		t.Fatalf("new blacklist request: %v", err)
	}
	blacklistReq.Header.Set("Content-Type", "application/json")
	blacklistResp, err := app.Test(blacklistReq)
	if err != nil {
		t.Fatalf("app.Test blacklist failed: %v", err)
	}
	t.Cleanup(func() {
		_ = blacklistResp.Body.Close()
	})
	if got := blacklistResp.StatusCode; got != http.StatusOK {
		t.Fatalf("blacklist status = %d, want %d", got, http.StatusOK)
	}
	var blacklistBody struct {
		Item struct {
			NodeID      string `json:"nodeId"`
			Blacklisted bool   `json:"blacklisted"`
		} `json:"item"`
	}
	if err := json.NewDecoder(blacklistResp.Body).Decode(&blacklistBody); err != nil {
		t.Fatalf("decode blacklist response: %v", err)
	}
	if blacklistBody.Item.NodeID != node.ID || !blacklistBody.Item.Blacklisted {
		t.Fatalf("blacklist response = %+v, want node blacklisted", blacklistBody)
	}

	releaseReq, err := http.NewRequest(http.MethodPost, "/api/v1/proxy/nodes/"+node.ID+"/release", nil)
	if err != nil {
		t.Fatalf("new release request: %v", err)
	}
	releaseResp, err := app.Test(releaseReq)
	if err != nil {
		t.Fatalf("app.Test release failed: %v", err)
	}
	t.Cleanup(func() {
		_ = releaseResp.Body.Close()
	})
	if got := releaseResp.StatusCode; got != http.StatusOK {
		t.Fatalf("release status = %d, want %d", got, http.StatusOK)
	}
	var releaseBody struct {
		Item struct {
			NodeID      string `json:"nodeId"`
			Blacklisted bool   `json:"blacklisted"`
		} `json:"item"`
	}
	if err := json.NewDecoder(releaseResp.Body).Decode(&releaseBody); err != nil {
		t.Fatalf("decode release response: %v", err)
	}
	if releaseBody.Item.NodeID != node.ID || releaseBody.Item.Blacklisted {
		t.Fatalf("release response = %+v, want node released", releaseBody)
	}
}
