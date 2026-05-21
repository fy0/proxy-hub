package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"proxy-hub/api/h"
	"proxy-hub/utils"
)

func TestSystemVersionRouteReturnsAppInfo(t *testing.T) {
	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	appInfo = &AppInfo{
		Name:        "ProxyHub",
		Version:     "9.8.7-test",
		Channel:     "test",
		PackageName: "proxy-hub",
	}
	t.Cleanup(func() {
		appInfo = nil
	})
	registerSystemRoutes(v1)

	resp := mustTestRequest(t, app, http.MethodGet, "/api/v1/system/version")
	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("status = %d, want %d", got, http.StatusOK)
	}

	var body struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Name != "ProxyHub" || body.Version != "9.8.7-test" || body.Channel != "test" {
		t.Fatalf("body = %+v", body)
	}
}

func TestGenOpenAPIIncludesSystemRoutes(t *testing.T) {
	cfg := &utils.AppConfig{
		APITitle:       "Proxy Hub API",
		APIVersion:     "test",
		OpenAPIEnabled: true,
		DocsPath:       "/docs",
	}
	app := fiber.New()
	api, v1 := h.NewAPI(app, cfg)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	registerSystemRoutes(v1)

	openapi, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal OpenAPI: %v", err)
	}
	spec := string(openapi)
	if !strings.Contains(spec, "/api/v1/system/version") {
		t.Fatalf("OpenAPI paths missing system version route")
	}
	if !strings.Contains(spec, "/api/v1/system/check-update") {
		t.Fatalf("OpenAPI paths missing system check-update route")
	}
	if !strings.Contains(spec, "/api/v1/system/listen") {
		t.Fatalf("OpenAPI paths missing system listen route")
	}
	if !strings.Contains(spec, "/api/v1/system/restart") {
		t.Fatalf("OpenAPI paths missing system restart route")
	}
	if !strings.Contains(spec, "updateCommand") {
		t.Fatalf("OpenAPI schema missing updateCommand field")
	}
	if !strings.Contains(spec, "distTag") {
		t.Fatalf("OpenAPI schema missing distTag field")
	}
}

func TestListenServeAtParseAndBuild(t *testing.T) {
	tests := []struct {
		name        string
		serveAt     string
		wantAddress string
		wantPort    int
		wantServeAt string
	}{
		{name: "all addresses", serveAt: ":3020", wantAddress: "", wantPort: 3020, wantServeAt: ":3020"},
		{name: "ipv4", serveAt: "127.0.0.1:3020", wantAddress: "127.0.0.1", wantPort: 3020, wantServeAt: "127.0.0.1:3020"},
		{name: "localhost", serveAt: "localhost:3020", wantAddress: "localhost", wantPort: 3020, wantServeAt: "localhost:3020"},
		{name: "ipv6", serveAt: "[::1]:3020", wantAddress: "::1", wantPort: 3020, wantServeAt: "[::1]:3020"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address, port, serveAt, err := parseListenServeAt(tt.serveAt)
			if err != nil {
				t.Fatalf("parseListenServeAt() error = %v", err)
			}
			if address != tt.wantAddress || port != tt.wantPort || serveAt != tt.wantServeAt {
				t.Fatalf("parseListenServeAt() = %q, %d, %q; want %q, %d, %q", address, port, serveAt, tt.wantAddress, tt.wantPort, tt.wantServeAt)
			}
		})
	}

	built, err := buildListenServeAt("::1", 4040)
	if err != nil {
		t.Fatalf("buildListenServeAt() error = %v", err)
	}
	if built != "[::1]:4040" {
		t.Fatalf("buildListenServeAt() = %q, want [::1]:4040", built)
	}
}

func TestListenServeAtRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		address string
		port    int
	}{
		{name: "zero port", address: "127.0.0.1", port: 0},
		{name: "port too high", address: "127.0.0.1", port: 65536},
		{name: "hostname", address: "example.com", port: 3020},
		{name: "invalid ip", address: "999.1.1.1", port: 3020},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildListenServeAt(tt.address, tt.port); err == nil {
				t.Fatalf("buildListenServeAt() error = nil, want error")
			}
		})
	}

	if _, _, _, err := parseListenServeAt("127.0.0.1"); err == nil {
		t.Fatalf("parseListenServeAt() error = nil, want error")
	}
}

func TestSystemListenRoutesReadAndUpdateConfig(t *testing.T) {
	cfg := setupSystemConfigTest(t)

	app := fiber.New()
	_, v1 := h.NewAPI(app, cfg)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})
	registerSystemRoutes(v1, systemRouteOptions{
		Config:         cfg,
		RunningServeAt: ":3020",
	})

	getResp := mustTestRequest(t, app, http.MethodGet, "/api/v1/system/listen")
	if got := getResp.StatusCode; got != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", got, http.StatusOK)
	}
	var current listenConfigBody
	if err := json.NewDecoder(getResp.Body).Decode(&current); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if current.ServeAt != ":3020" || current.ListenPort != 3020 || current.RestartRequired {
		t.Fatalf("GET body = %+v, want current :3020 without restart requirement", current)
	}

	updateResp := mustJSONTestRequest(t, app, http.MethodPut, "/api/v1/system/listen", `{"listenAddress":"127.0.0.1","listenPort":4040}`)
	if got := updateResp.StatusCode; got != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", got, http.StatusOK)
	}
	var updated struct {
		Message string           `json:"message"`
		Item    listenConfigBody `json:"item"`
	}
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if updated.Item.ServeAt != "127.0.0.1:4040" || !updated.Item.RestartRequired {
		t.Fatalf("PUT body = %+v, want saved 127.0.0.1:4040 with restart requirement", updated.Item)
	}
	if cfg.ServeAt != "127.0.0.1:4040" {
		t.Fatalf("cfg.ServeAt = %q, want updated value", cfg.ServeAt)
	}

	reloaded := utils.ReadConfig()
	if reloaded.ServeAt != "127.0.0.1:4040" {
		t.Fatalf("reloaded ServeAt = %q, want persisted value", reloaded.ServeAt)
	}
}

func TestSystemListenUpdateRejectsInvalidConfig(t *testing.T) {
	cfg := setupSystemConfigTest(t)

	app := fiber.New()
	_, v1 := h.NewAPI(app, cfg)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})
	registerSystemRoutes(v1, systemRouteOptions{Config: cfg})

	resp := mustJSONTestRequest(t, app, http.MethodPut, "/api/v1/system/listen", `{"listenAddress":"example.com","listenPort":3020}`)
	if got := resp.StatusCode; got != http.StatusBadRequest {
		t.Fatalf("PUT invalid address status = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestSystemRestartRequiresConfirmationAndCallsCallback(t *testing.T) {
	app := fiber.New()
	_, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	called := false
	registerSystemRoutes(v1, systemRouteOptions{
		Config:         &utils.AppConfig{ServeAt: ":3020"},
		RunningServeAt: ":3020",
		RequestRestart: func() error {
			called = true
			return nil
		},
	})

	rejected := mustJSONTestRequest(t, app, http.MethodPost, "/api/v1/system/restart", `{"confirm":false}`)
	if got := rejected.StatusCode; got != http.StatusBadRequest {
		t.Fatalf("restart without confirm status = %d, want %d", got, http.StatusBadRequest)
	}
	if called {
		t.Fatalf("restart callback was called without confirmation")
	}

	accepted := mustJSONTestRequest(t, app, http.MethodPost, "/api/v1/system/restart", `{"confirm":true}`)
	if got := accepted.StatusCode; got != http.StatusAccepted {
		t.Fatalf("restart with confirm status = %d, want %d", got, http.StatusAccepted)
	}
	if !called {
		t.Fatalf("restart callback was not called")
	}
}

func setupSystemConfigTest(t *testing.T) *utils.AppConfig {
	t.Helper()

	tempDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	cfg := utils.ReadConfig()
	cfg.PrintConfig = false
	cfg.ServeAt = ":3020"
	if err := utils.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	return cfg
}

func mustJSONTestRequest(t *testing.T, app *fiber.App, method, target, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, target, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test %s %s: %v", method, target, err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})
	return resp
}

func TestRequestBodyLimitBytesDefaultsTo64MB(t *testing.T) {
	tests := []struct {
		name string
		cfg  *utils.AppConfig
		want int
	}{
		{name: "nil config", cfg: nil, want: defaultBodyLimitBytes},
		{name: "zero limit", cfg: &utils.AppConfig{}, want: defaultBodyLimitBytes},
		{name: "below default", cfg: &utils.AppConfig{AttachmentSizeLimit: 8192}, want: defaultBodyLimitBytes},
		{name: "at default", cfg: &utils.AppConfig{AttachmentSizeLimit: 65536}, want: defaultBodyLimitBytes},
		{name: "above default", cfg: &utils.AppConfig{AttachmentSizeLimit: 131072}, want: 128 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestBodyLimitBytes(tt.cfg); got != tt.want {
				t.Fatalf("requestBodyLimitBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}
