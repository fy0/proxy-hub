package api

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/contrib/fiberzap/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"

	"go-template/api/h"
	"go-template/api/user"
	"go-template/utils"
)

// Init 初始化 Fiber + Huma 的初始化，启动 HTTP 服务
func Init(ctx context.Context, cfg *utils.AppConfig, assets embed.FS) error {
	theLogger := utils.LoggerFromContext(ctx)

	bodyLimit := int(cfg.AttachmentSizeLimit * 1024)
	if bodyLimit < 1*1024*1024 {
		bodyLimit = 1 * 1024 * 1024
	}

	app := fiber.New(fiber.Config{
		BodyLimit:             bodyLimit,
		DisableStartupMessage: true,
		Immutable:             true,
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CorsAllowOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,PATCH",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowCredentials: cfg.CorsAllowOrigins != "*",
	}))

	// 使用 fiberzap 统一日志输出
	app.Use(fiberzap.New(fiberzap.Config{
		Logger: theLogger,
		// 按状态码区分日志级别：500+ Error, 400+ Warn, 其他 Info
		Levels: []zapcore.Level{zapcore.ErrorLevel, zapcore.WarnLevel, zapcore.InfoLevel},
	}))

	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e any) {
			theLogger.Error("panic recovered",
				zap.Any("error", e),
				zap.String("method", c.Method()),
				zap.String("path", c.Path()),
				zap.Stack("stacktrace"),
			)
		},
	}))
	app.Use(compress.New())

	api, v1 := h.NewAPI(app, cfg)
	api.UseMiddleware(h.HumaTraceMiddleware)
	h.HumaTypesRegister()
	h.HumaValidatePatch()

	user.Register(v1)
	registerHealthRoutes(v1)
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

	app.Use(mountPath, filesystem.New(filesystem.Config{
		Root:       fs,
		PathPrefix: "static",
		MaxAge:     300,
	}))
}

// GenOpenAPI 生成 OpenAPI JSON/YAML 文件
func GenOpenAPI(ctx context.Context, cfg *utils.AppConfig, assets embed.FS, outputPath string) {
	theLogger := utils.LoggerFromContext(ctx)

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
	api.UseMiddleware(h.HumaTraceMiddleware)
	h.HumaTypesRegister()
	h.HumaValidatePatch()

	// 注册所有路由
	user.Register(v1)
	registerHealthRoutes(v1)

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
