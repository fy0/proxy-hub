package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"gorm.io/gorm/logger"

	"proxy-hub/model"
	proxyService "proxy-hub/service/proxy"
)

func TestMappingIPLookupHandlerUnavailableMapping(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		wantError := "port mapping is disabled"
		if enabled {
			name = "stopped"
			wantError = "port mapping runtime is not running"
		}
		t.Run(name, func(t *testing.T) {
			if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(model.DBClose)
			if err := proxyService.RuntimeStop(); err != nil {
				t.Fatal(err)
			}
			req := proxyService.MappingUpsertRequest{
				Enabled:          enabled,
				ListenAddress:    "127.0.0.1",
				ListenPort:       10081,
				OutboundProtocol: proxyService.OutboundProtocolMixed,
				Strategy:         proxyService.StrategyManual,
			}
			mapping, err := proxyService.MappingCreate(context.Background(), nil, req)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := proxyService.MappingUpdate(context.Background(), nil, mapping.ID, req); err != nil {
				t.Fatal(err)
			}
			app, apiGroup := newProxyAPITestApp(t)
			Register(apiGroup)
			resp := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/mappings/"+mapping.ID+"/ip-lookup", nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var result proxyService.IPLookupResultDTO
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result.IP != "" || result.Error != wantError || result.CheckedAt.IsZero() {
				t.Fatalf("unexpected result: %+v", result)
			}
			missing := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/mappings/missing/ip-lookup", nil)
			defer missing.Body.Close()
			if missing.StatusCode != http.StatusNotFound {
				t.Fatalf("missing mapping status = %d, want 404", missing.StatusCode)
			}
		})
	}
}
