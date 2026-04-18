package api

import (
	"embed"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"go-template/utils"
)

func TestMountStaticDisablesCacheForIndexOnly(t *testing.T) {
	t.Parallel()

	app := newMountedStaticTestApp(t, "/")

	assertIndexNoCache(t, mustTestRequest(t, app, http.MethodGet, "/"), "/")
	assertIndexNoCache(t, mustTestRequest(t, app, http.MethodGet, "/index.html"), "/index.html")
	assertStaticAssetLongCache(t, mustTestRequest(t, app, http.MethodGet, "/app.js"), "/app.js")
	assertStaticAssetLongCache(t, mustTestRequest(t, app, http.MethodGet, "/favicon.ico"), "/favicon.ico")
}

func TestMountStaticDisablesCacheForIndexOnCustomWebURL(t *testing.T) {
	t.Parallel()

	app := newMountedStaticTestApp(t, "/kanban/")

	assertIndexNoCache(t, mustTestRequest(t, app, http.MethodGet, "/kanban"), "/kanban")
	assertIndexNoCache(t, mustTestRequest(t, app, http.MethodGet, "/kanban/index.html"), "/kanban/index.html")
	assertStaticAssetLongCache(t, mustTestRequest(t, app, http.MethodGet, "/kanban/app.js"), "/kanban/app.js")
	assertStaticAssetLongCache(t, mustTestRequest(t, app, http.MethodGet, "/kanban/favicon.ico"), "/kanban/favicon.ico")
}

func newMountedStaticTestApp(t *testing.T, webURL string) *fiber.App {
	t.Helper()

	cfg := &utils.AppConfig{
		WebUrl:      webURL,
		UIOverwrite: filepath.Join("testdata", "static-cache"),
	}

	app := fiber.New()
	mountStatic(app, cfg, embed.FS{}, zap.NewNop())
	t.Cleanup(func() {
		_ = app.Shutdown()
	})
	return app
}

func assertIndexNoCache(t *testing.T, resp *http.Response, target string) {
	t.Helper()

	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("%s status = %d, want %d", target, got, http.StatusOK)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("%s Cache-Control = %q", target, got)
	}
	if got := resp.Header.Get("Pragma"); got != "no-cache" {
		t.Fatalf("%s Pragma = %q", target, got)
	}
	if got := resp.Header.Get("Expires"); got != "0" {
		t.Fatalf("%s Expires = %q", target, got)
	}
}

func assertStaticAssetLongCache(t *testing.T, resp *http.Response, target string) {
	t.Helper()

	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("%s status = %d, want %d", target, got, http.StatusOK)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=2592000" {
		t.Fatalf("%s Cache-Control = %q", target, got)
	}
	if got := resp.Header.Get("Pragma"); got != "" {
		t.Fatalf("%s Pragma = %q", target, got)
	}
	if got := resp.Header.Get("Expires"); got != "" {
		t.Fatalf("%s Expires = %q", target, got)
	}
}

func mustTestRequest(t *testing.T, app *fiber.App, method, target string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, target, err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test %s %s: %v", method, target, err)
	}
	t.Cleanup(func() {
		_ = resp.Body.Close()
	})
	return resp
}
