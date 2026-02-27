package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"go-template/api/h"
)

// registerHealthRoutes 注册健康探测接口，用于服务监控
func registerHealthRoute(api huma.API, path, operationID string) {
	h.HumaRegister(api, huma.Operation{
		OperationID: operationID,
		Tags:        []string{"health-健康探测"},
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "健康探测",
	}, func(ctx context.Context, _ *struct{}) (*h.MessageResponse, error) {
		return h.NewMessageResponse("ok"), nil
	})
}
