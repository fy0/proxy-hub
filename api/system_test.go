package api

import (
	"encoding/json"
	"net/http"
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
	if !strings.Contains(spec, "updateCommand") {
		t.Fatalf("OpenAPI schema missing updateCommand field")
	}
	if !strings.Contains(spec, "distTag") {
		t.Fatalf("OpenAPI schema missing distTag field")
	}
}
