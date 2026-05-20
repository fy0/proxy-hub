package h

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v2"

	"proxy-hub/utils"
)

// NewAPI 会基于 Fiber 实例构建 Huma API，并按约定创建 /api/v1 分组。
func NewAPI(app *fiber.App, cfg *utils.AppConfig) (huma.API, *huma.Group) {
	title := cfg.APITitle
	if title == "" {
		title = "Proxy Hub API"
	}

	version := cfg.APIVersion
	if version == "" {
		version = "1.0.0"
	}

	apiConfig := huma.DefaultConfig(title, version)

	if !cfg.OpenAPIEnabled {
		apiConfig.OpenAPIPath = ""
		apiConfig.DocsPath = ""
	}

	if cfg.OpenAPIEnabled && cfg.DocsPath != "" {
		apiConfig.DocsPath = cfg.DocsPath
	}

	api := humafiber.New(app, apiConfig)
	api.UseMiddleware(HumaTraceMiddleware)
	group := huma.NewGroup(api, "/api/v1")

	// 默认比较严格，根据情况开启吧
	// 允许接口提交额外参数，不至于多提一个就会报错
	// humaConfig.OnAddOperation = append(humaConfig.OnAddOperation, func(oapi *huma.OpenAPI, op *huma.Operation) {
	// 	for _, schema := range oapi.Components.Schemas.Map() {
	// 		schema.AdditionalProperties = true
	// 	}
	// })

	return api, group
}

func HumaTypesRegister() {
	// 注册 any 接口类型的 Schema，使其在文档中表现为任意对象
	huma.RegisterTypeSchema(reflect.TypeOf((*any)(nil)).Elem(), func(huma.Registry) *huma.Schema {
		return &huma.Schema{
			OneOf: []*huma.Schema{
				{Type: huma.TypeString},
				{Type: huma.TypeNumber},
				{Type: huma.TypeBoolean},
				{Type: huma.TypeObject, AdditionalProperties: true},
				{Type: huma.TypeArray, Items: &huma.Schema{}},
			},
		}
	})

	// 处理 []any
	huma.RegisterTypeSchema(reflect.TypeOf([]any{}), func(huma.Registry) *huma.Schema {
		return &huma.Schema{
			Type: "array",
			Items: &huma.Schema{
				Type:                 "object",
				AdditionalProperties: map[string]*huma.Schema{},
			},
		}
	})
}
