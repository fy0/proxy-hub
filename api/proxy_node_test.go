package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm/logger"

	"proxy-hub/api/h"
	proxyAPI "proxy-hub/api/proxy"
	"proxy-hub/model"
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
