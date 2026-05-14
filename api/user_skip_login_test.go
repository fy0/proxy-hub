package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"proxy-hub/api/h"
	userAPI "proxy-hub/api/user"
	"proxy-hub/model"
	"proxy-hub/utils"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm/logger"
)

func TestUserSkipLoginCreatesRootSession(t *testing.T) {
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
	userAPI.Register(v1)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	resp := mustTestRequest(t, app, http.MethodPost, "/api/v1/user/skip-login")
	if got := resp.StatusCode; got != http.StatusOK {
		t.Fatalf("skip login status = %d, want %d", got, http.StatusOK)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode skip login response: %v", err)
	}
	if body.Token == "" {
		t.Fatal("skip login returned empty token")
	}

	req, err := http.NewRequest(http.MethodGet, "/api/v1/user/info", nil)
	if err != nil {
		t.Fatalf("new user info request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+body.Token)
	infoResp, err := app.Test(req)
	if err != nil {
		t.Fatalf("user info request failed: %v", err)
	}
	t.Cleanup(func() {
		_ = infoResp.Body.Close()
	})

	if got := infoResp.StatusCode; got != http.StatusOK {
		t.Fatalf("user info status = %d, want %d", got, http.StatusOK)
	}
}
