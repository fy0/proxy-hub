package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"go-template/api/h"
	"go-template/api/user"
	"go-template/utils"
)

// Init 初始化 Fiber + Huma 的初始化，启动 HTTP 服务
func Init(ctx context.Context, cfg *utils.AppConfig, assets embed.FS) error {
	_ = ctx
	theLogger := utils.Logger

	bodyLimit := int(cfg.AttachmentSizeLimit * 1024)
	if bodyLimit < 1*1024*1024 {
		bodyLimit = 1 * 1024 * 1024
	}

	app := fiber.New(fiber.Config{
		BodyLimit:             bodyLimit,
		DisableStartupMessage: true,
		Immutable:             true,
	})

	allowOrigins := strings.TrimSpace(cfg.CorsAllowOrigins)
	if allowOrigins == "" {
		allowOrigins = "*"
	}

	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowCredentials: allowOrigins != "*" && !strings.Contains(allowOrigins, "*"),
	}))

	// 自定义日志中间件
	// 格式: 时间 | 级别 | 消息 | {JSON字段} | handler位置
	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		latency := time.Since(start)
		status := c.Response().StatusCode()

		// 跳过静态资源
		path := c.Path()
		if path == "/hello" || path == "/favicon.ico" {
			return err
		}

		// 获取 handler 位置
		handlerInfo := "-"
		if hInfo := c.Locals("humaHandlerInfo"); hInfo != nil {
			info := hInfo.(*h.HandlerInfo)
			handlerInfo = fmt.Sprintf("%s:%d", info.FilePath, info.Line)
		}

		// 构建 JSON 字段
		jsonFields := fmt.Sprintf(`{"latency": "%v", "status": %d, "method": "%s", "url": "%s"}`,
			latency, status, c.Method(), path)

		// 根据状态码选择日志级别
		switch {
		case status >= 500:
			theLogger.Error(fmt.Sprintf("%d | %s | %s", status, jsonFields, handlerInfo))
		case status >= 400:
			theLogger.Debug(fmt.Sprintf("%d | %s | %s", status, jsonFields, handlerInfo))
		default:
			theLogger.Info(fmt.Sprintf("%d | %s | %s", status, jsonFields, handlerInfo))
		}

		return err
	})

	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	app.Use(compress.New())

	api, v1 := h.NewAPI(app, cfg)
	registerHealthRoutes(api)
	h.HumaTypesRegister()
	h.HumaValidatePatch()

	user.Register(v1)
	mountStatic(app, cfg, assets, theLogger)

	return app.Listen(cfg.ServeAt)
}

// mountStatic 将内置静态资源或自定义目录挂载到 Fiber 上
func mountStatic(app *fiber.App, cfg *utils.AppConfig, assets embed.FS, logger *zap.Logger) {
	var fs http.FileSystem

	if cfg.UIOverwrite != "" {
		if _, err := os.Stat(cfg.UIOverwrite); err != nil {
			logger.Warn("自定义前端目录不存在，回退到内置资源", zap.String("path", cfg.UIOverwrite), zap.Error(err))
		} else {
			fs = http.Dir(cfg.UIOverwrite)
		}
	}

	if fs == nil {
		fs = http.FS(assets)
	}

	mountPath := cfg.WebUrl
	if mountPath == "" {
		mountPath = "/"
	}

	handler := filesystem.New(filesystem.Config{
		Root:       fs,
		PathPrefix: "static",
		MaxAge:     300,
	})

	// 静态资源作为 Fiber 底层路由兜底，不依赖 Fiber middleware 链。
	mountPath = strings.TrimSuffix(mountPath, "/")
	if mountPath == "" {
		mountPath = "/"
	}

	if mountPath == "/" {
		app.All("/*", handler)
		return
	}

	app.All(mountPath, handler)
	app.All(mountPath+"/*", handler)
}

// GenOpenAPI 生成 OpenAPI JSON/YAML 文件
func GenOpenAPI(ctx context.Context, cfg *utils.AppConfig, assets embed.FS, outputPath string) {
	_ = ctx
	theLogger := utils.Logger

	bodyLimit := int(cfg.AttachmentSizeLimit * 1024)
	if bodyLimit < 1*1024*1024 {
		bodyLimit = 1 * 1024 * 1024
	}

	app := fiber.New(fiber.Config{
		BodyLimit:             bodyLimit,
		DisableStartupMessage: true,
		Immutable:             true,
	})

	// 创建 Huma API 实例
	api, v1 := h.NewAPI(app, cfg)
	h.HumaTypesRegister()
	h.HumaValidatePatch()

	// 注册所有路由
	user.Register(v1)
	registerHealthRoute(api, "/health", "health-get")

	// 获取 OpenAPI 规范
	openapi := api.OpenAPI()

	// 添加生成时间到扩展字段
	if openapi.Info.Extensions == nil {
		openapi.Info.Extensions = make(map[string]any)
	}
	openapi.Info.Extensions["x-generated-at"] = time.Now().Format("2006-01-02 15:04:05")

	// 根据文件扩展名决定输出格式
	var openapiBytes []byte
	var err error

	if strings.HasSuffix(outputPath, ".json") {
		openapiBytes, err = json.MarshalIndent(openapi, "", "  ")
		if err != nil {
			theLogger.Fatal("生成 OpenAPI JSON 失败", zap.Error(err))
		}
	} else {
		// 默认使用 YAML 格式
		if !strings.HasSuffix(outputPath, ".yaml") && !strings.HasSuffix(outputPath, ".yml") {
			outputPath = strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".yaml"
		}
		openapiBytes, err = yaml.Marshal(openapi)
		if err != nil {
			theLogger.Fatal("生成 OpenAPI YAML 失败", zap.Error(err))
		}
	}

	err = os.WriteFile(outputPath, openapiBytes, 0644)
	if err != nil {
		theLogger.Fatal("写入 OpenAPI 文件失败", zap.Error(err), zap.String("path", outputPath))
	}

	theLogger.Info("OpenAPI 规范已保存", zap.String("path", outputPath))
}
