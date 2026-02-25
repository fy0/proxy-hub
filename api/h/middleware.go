package h

import (
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.uber.org/zap"
)

type corsSettings struct {
	allowOrigins     string
	allowMethods     string
	allowHeaders     string
	allowCredentials bool
}

func newCORSSettings(allowOrigins string) corsSettings {
	allowOrigins = strings.TrimSpace(allowOrigins)
	if allowOrigins == "" {
		allowOrigins = "*"
	}
	return corsSettings{
		allowOrigins:     allowOrigins,
		allowMethods:     "GET,POST,PUT,DELETE,PATCH,OPTIONS",
		allowHeaders:     "Origin, Content-Type, Accept, Authorization",
		allowCredentials: allowOrigins != "*" && !strings.Contains(allowOrigins, "*"),
	}
}

func setCORSHeaders(ctx huma.Context, cfg corsSettings) {
	origin := strings.TrimSpace(ctx.Header("Origin"))
	if cfg.allowOrigins == "*" || origin == "" {
		ctx.SetHeader("Access-Control-Allow-Origin", cfg.allowOrigins)
	} else {
		for _, allowed := range strings.Split(cfg.allowOrigins, ",") {
			if strings.TrimSpace(allowed) == origin {
				ctx.SetHeader("Access-Control-Allow-Origin", origin)
				ctx.AppendHeader("Vary", "Origin")
				break
			}
		}
	}

	ctx.SetHeader("Access-Control-Allow-Methods", cfg.allowMethods)
	ctx.SetHeader("Access-Control-Allow-Headers", cfg.allowHeaders)

	if cfg.allowCredentials {
		ctx.SetHeader("Access-Control-Allow-Credentials", "true")
	}
}

func NewHumaCORSMiddleware(allowOrigins string) func(ctx huma.Context, next func(huma.Context)) {
	cfg := newCORSSettings(allowOrigins)
	return func(ctx huma.Context, next func(huma.Context)) {
		setCORSHeaders(ctx, cfg)
		next(ctx)
	}
}

func NewHumaRecoverMiddleware(logger *zap.Logger) func(ctx huma.Context, next func(huma.Context)) {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		defer func() {
			if r := recover(); r != nil {
				url := ctx.URL()
				logger.Error("panic recovered",
					zap.Any("error", r),
					zap.String("method", ctx.Method()),
					zap.String("path", url.Path),
					zap.Stack("stacktrace"),
				)

				ctx.SetStatus(http.StatusInternalServerError)
				ctx.SetHeader("Content-Type", "application/json")
				_, _ = ctx.BodyWriter().Write([]byte(`{"code":"INTERNAL_ERROR","message":"internal server error"}`))
			}
		}()

		next(ctx)
	}
}

func NewHumaLoggerMiddleware(logger *zap.Logger) func(ctx huma.Context, next func(huma.Context)) {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		start := time.Now()
		next(ctx)
		cost := time.Since(start)

		url := ctx.URL()

		fields := []zap.Field{
			zap.String("method", ctx.Method()),
			zap.String("path", url.Path),
			zap.Int("status", ctx.Status()),
			zap.Duration("cost", cost),
		}

		if op := ctx.Operation(); op != nil && op.OperationID != "" {
			fields = append(fields, zap.String("operationId", op.OperationID))
		}

		switch {
		case ctx.Status() >= 500:
			logger.Error("http request", fields...)
		case ctx.Status() >= 400:
			logger.Warn("http request", fields...)
		default:
			logger.Info("http request", fields...)
		}
	}
}
