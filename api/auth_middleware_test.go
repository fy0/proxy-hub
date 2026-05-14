package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v2"

	"proxy-hub/api/h"
	"proxy-hub/utils"
)

func TestUserMiddlewareOnlyAppliesWhenRouteRegistersIt(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	api, v1 := h.NewAPI(app, &utils.AppConfig{
		APITitle:   "Proxy Hub API",
		APIVersion: "test",
		DocsPath:   "/docs",
	})
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	registerHealthRoute(api, "/health", "test-health-public")
	h.HumaRegister(v1, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/private",
		OperationID: "test-private-auth-required",
		Middlewares: huma.Middlewares{h.HumaUserMiddleware},
	}, func(context.Context, *struct{}) (*h.MessageResponse, error) {
		return h.NewMessageResponse("private"), nil
	})

	publicResp := mustTestRequest(t, app, http.MethodGet, "/health")
	if got := publicResp.StatusCode; got != http.StatusOK {
		t.Fatalf("public route status = %d, want %d", got, http.StatusOK)
	}

	privateResp := mustTestRequest(t, app, http.MethodGet, "/api/v1/private")
	if got := privateResp.StatusCode; got != http.StatusBadRequest {
		t.Fatalf("protected route status = %d, want %d", got, http.StatusBadRequest)
	}
}
