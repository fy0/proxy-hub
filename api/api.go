package api

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"go.uber.org/zap"

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
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CorsAllowOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,PATCH",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowCredentials: cfg.CorsAllowOrigins != "*",
	}))
	app.Use(getLinePrint())

	app.Use(recover.New(recover.Config{EnableStackTrace: true}))
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
		PathPrefix: "",
		MaxAge:     300,
	}))
}

func getLinePrint() fiber.Handler {
	isSrcPath := false
	if _, err := os.Stat("./main.go"); err == nil {
		isSrcPath = true
	}

	return logger.New(logger.Config{
		Format: "${time} | ${status} | ${latency} | ${ip} | ${method} | ${path} | ${file_link}\n",
		CustomTags: map[string]logger.LogFunc{
			"file_link": func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				if !isSrcPath {
					return output.WriteString("-")
				}

				// 获取当前请求的路由信息
				route := c.Route()
				if route == nil {
					return output.WriteString("-")
				}

				var outputStr string
				handler := route.Handlers[len(route.Handlers)-1]
				v := reflect.ValueOf(handler)

				if v.Kind() == reflect.Func {
					srcPath, _ := runtime.FuncForPC(v.Pointer()).FileLine(v.Pointer())
					if strings.HasSuffix(srcPath, "humafiber.go") {
						hInfo := c.Locals("humaHandlerInfo")
						if hInfo != nil {
							hInfo := hInfo.(*h.HandlerInfo)
							outputStr = fmt.Sprintf("%s:%d", hInfo.FilePath, hInfo.Line)
						}
					}
				}

				if outputStr == "" && v.Kind() == reflect.Func {
					srcPath, line := runtime.FuncForPC(v.Pointer()).FileLine(v.Pointer())

					// 使用相对路径
					wd, _ := os.Getwd()
					relPath, err := filepath.Rel(wd, srcPath)
					if err != nil {
						return output.WriteString("-")
					}

					outputStr = fmt.Sprintf("%s:%d", relPath, line)
				}

				return output.WriteString(outputStr)
			},
		},
	})
}
