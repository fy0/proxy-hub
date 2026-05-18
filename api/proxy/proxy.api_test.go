package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v2"
	"proxy-hub/api/h"
	"proxy-hub/model"
	proxyService "proxy-hub/service/proxy"
	"proxy-hub/utils"

	"gorm.io/gorm/logger"
)

func TestReloadRuntimeAfterMutationIgnoresBindFailure(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(func() {
		_ = proxyService.RuntimeStop()
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(:0) failed: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}

	_, err = proxyService.MappingCreate(context.Background(), nil, proxyService.MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       uint16(addr.Port),
		OutboundProtocol: proxyService.OutboundProtocolMixed,
		Strategy:         proxyService.StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	if err := reloadRuntimeAfterMutation(); err != nil {
		t.Fatalf("reloadRuntimeAfterMutation() error = %v, want nil", err)
	}
}

func TestSettingsExportImportHandlersRoundTrip(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(func() {
		_ = proxyService.RuntimeStop()
	})

	ctx := context.Background()
	node, err := proxyService.NodeCreate(ctx, nil, proxyService.NodeUpsertRequest{
		Name:     "edge",
		Protocol: proxyService.ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     uint16Ptr(1080),
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	_, err = proxyService.MappingCreate(ctx, nil, proxyService.MappingUpsertRequest{
		Enabled:          true,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10091,
		OutboundProtocol: proxyService.OutboundProtocolMixed,
		Strategy:         proxyService.StrategyManual,
		NodeIDs:          []string{node.ID},
		ActiveNodeID:     &node.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	app, apiGroup := newProxyAPITestApp(t)
	Register(apiGroup)

	exportResp := mustProxyAPITestRequest(t, app, http.MethodGet, "/api/v1/proxy/settings/export", nil)
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d, want %d", exportResp.StatusCode, http.StatusOK)
	}
	var backup proxyService.SettingsBackupDTO
	if err := json.NewDecoder(exportResp.Body).Decode(&backup); err != nil {
		t.Fatalf("decode export response: %v", err)
	}
	if backup.Kind != proxyService.SettingsBackupKind || len(backup.Data.Nodes) != 1 || len(backup.Data.Mappings) != 1 {
		t.Fatalf("backup = %+v, want exported node and mapping", backup)
	}

	extra, err := proxyService.NodeCreate(ctx, nil, proxyService.NodeUpsertRequest{
		Name:     "extra",
		Protocol: proxyService.ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     uint16Ptr(1081),
	})
	if err != nil {
		t.Fatalf("NodeCreate(extra) error = %v", err)
	}

	body, err := json.Marshal(backup)
	if err != nil {
		t.Fatalf("json.Marshal(backup) error = %v", err)
	}
	importResp := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/settings/import", body)
	if importResp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d, want %d", importResp.StatusCode, http.StatusOK)
	}
	if _, err := proxyService.NodeGet(ctx, nil, extra.ID); err != proxyService.ErrNodeNotFound {
		t.Fatalf("NodeGet(extra) error = %v, want %v", err, proxyService.ErrNodeNotFound)
	}
}

func TestMappingTestHandlerReturnsDisabledResult(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(func() {
		_ = proxyService.RuntimeStop()
	})

	mapping, err := proxyService.MappingCreate(context.Background(), nil, proxyService.MappingUpsertRequest{
		Enabled:          false,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10081,
		OutboundProtocol: proxyService.OutboundProtocolMixed,
		Strategy:         proxyService.StrategyManual,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	app, apiGroup := newProxyAPITestApp(t)
	Register(apiGroup)

	body, err := json.Marshal(proxyService.ProxyTestRequest{ProbeURL: "https://example.com/generate_204"})
	if err != nil {
		t.Fatalf("json.Marshal request error = %v", err)
	}
	resp := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/mappings/"+mapping.ID+"/test", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result proxyService.ProxyTestResultDTO
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Available {
		t.Fatalf("Available = true, want false")
	}
	if result.TargetType != "mapping" || result.TargetID != mapping.ID || result.ProbeURL != "https://example.com/generate_204" {
		t.Fatalf("result = %+v, want mapping test result", result)
	}
	if result.Error == "" {
		t.Fatalf("Error is empty, want disabled reason")
	}
}

func TestMappingSwitchHandlerUpdatesActiveRoute(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)
	t.Cleanup(func() {
		_ = proxyService.RuntimeStop()
	})

	ctx := context.Background()
	port := uint16(1080)
	first, err := proxyService.NodeCreate(ctx, nil, proxyService.NodeUpsertRequest{
		Name:     "first",
		Protocol: proxyService.ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(first) error = %v", err)
	}
	second, err := proxyService.NodeCreate(ctx, nil, proxyService.NodeUpsertRequest{
		Name:     "second",
		Protocol: proxyService.ProtocolSOCKS5,
		Server:   "127.0.0.2",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate(second) error = %v", err)
	}
	mapping, err := proxyService.MappingCreate(ctx, nil, proxyService.MappingUpsertRequest{
		Enabled:          false,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10082,
		OutboundProtocol: proxyService.OutboundProtocolMixed,
		Strategy:         proxyService.StrategyManual,
		NodeIDs:          []string{first.ID, second.ID},
		ActiveNodeID:     &first.ID,
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	app, apiGroup := newProxyAPITestApp(t)
	Register(apiGroup)

	body, err := json.Marshal(proxyService.MappingSwitchRequest{
		TargetType: proxyService.MappingSwitchTargetNode,
		TargetID:   second.ID,
	})
	if err != nil {
		t.Fatalf("json.Marshal request error = %v", err)
	}
	resp := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/mappings/"+mapping.ID+"/switch", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var output struct {
		Item *proxyService.PortMappingDTO `json:"item"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if output.Item == nil || output.Item.ActiveNodeID == nil || *output.Item.ActiveNodeID != second.ID {
		t.Fatalf("response item = %+v, want active node %q", output.Item, second.ID)
	}
}

func TestMappingSwitchHandlerRejectsNonManualMapping(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	ctx := context.Background()
	port := uint16(1080)
	node, err := proxyService.NodeCreate(ctx, nil, proxyService.NodeUpsertRequest{
		Name:     "first",
		Protocol: proxyService.ProtocolSOCKS5,
		Server:   "127.0.0.1",
		Port:     &port,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}
	mapping, err := proxyService.MappingCreate(ctx, nil, proxyService.MappingUpsertRequest{
		Enabled:          false,
		ListenAddress:    "127.0.0.1",
		ListenPort:       10083,
		OutboundProtocol: proxyService.OutboundProtocolMixed,
		Strategy:         proxyService.StrategyLeastLatency,
		NodeIDs:          []string{node.ID},
	})
	if err != nil {
		t.Fatalf("MappingCreate() error = %v", err)
	}

	app, apiGroup := newProxyAPITestApp(t)
	Register(apiGroup)

	body, err := json.Marshal(proxyService.MappingSwitchRequest{
		TargetType: proxyService.MappingSwitchTargetNode,
		TargetID:   node.ID,
	})
	if err != nil {
		t.Fatalf("json.Marshal request error = %v", err)
	}
	resp := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/mappings/"+mapping.ID+"/switch", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestNodeTestHandlerRejectsInvalidProbeURL(t *testing.T) {
	if err := model.InitWithDSN(":memory:", int(logger.Silent), true); err != nil {
		t.Fatalf("InitWithDSN(:memory:) failed: %v", err)
	}
	t.Cleanup(model.DBClose)

	nodePort := uint16(1080)
	node, err := proxyService.NodeCreate(context.Background(), nil, proxyService.NodeUpsertRequest{
		Name:     "edge",
		Protocol: proxyService.ProtocolHTTP,
		Server:   "127.0.0.1",
		Port:     &nodePort,
	})
	if err != nil {
		t.Fatalf("NodeCreate() error = %v", err)
	}

	app, apiGroup := newProxyAPITestApp(t)
	Register(apiGroup)

	body, err := json.Marshal(proxyService.ProxyTestRequest{ProbeURL: "ftp://example.com/file"})
	if err != nil {
		t.Fatalf("json.Marshal request error = %v", err)
	}
	resp := mustProxyAPITestRequest(t, app, http.MethodPost, "/api/v1/proxy/nodes/"+node.ID+"/test", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func newProxyAPITestApp(t *testing.T) (*fiber.App, huma.API) {
	t.Helper()
	app := fiber.New()
	_, apiGroup := h.NewAPI(app, &utils.AppConfig{APITitle: "test", APIVersion: "1.0.0"})
	t.Cleanup(func() {
		_ = app.Shutdown()
	})
	return app, apiGroup
}

func mustProxyAPITestRequest(t *testing.T, app *fiber.App, method string, target string, body []byte) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, target, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

func uint16Ptr(value uint16) *uint16 {
	return &value
}
