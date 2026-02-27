package h

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"

	"go-template/model/tables"
	userService "go-template/service/user"
)

// getToken 从 Fiber Context 获取 Token
// 优先级: 1. Authorization Header  2. Authorization Cookie  3. Query token
func getToken(c *fiber.Ctx) string {
	// 1. 从 Header 获取 (支持 Bearer)
	auth := c.Get("Authorization")
	if auth != "" {
		if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		return strings.TrimSpace(auth)
	}

	// 2. 从 Cookie 获取
	if cookie := c.Cookies("Authorization"); cookie != "" {
		return cookie
	}

	// 3. 从 Query 获取（方便 WebSocket/测试）
	return c.Query("token")
}

// GetUserInfo 从 Huma Context 中读取用户信息。
func GetUserInfo(ctx context.Context) *tables.UserTable {
	userInfo, _ := ctx.Value("user").(*tables.UserTable)
	return userInfo
}

func HumaUserMiddleware(ctx huma.Context, next func(huma.Context)) {
	// 注: huma 的中间件能力比较菜，拿cookie和header都费劲，所以选择从fiber中获取
	// 获取底层的 fiber context
	fiberCtx := humafiber.Unwrap(ctx)
	token := getToken(fiberCtx)

	if token == "" {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.SetHeader("Content-Type", "application/json")
		ctx.BodyWriter().Write([]byte(`{"code":"MISSING_TOKEN","message":"缺少认证凭证"}`))
		return
	}

	u, err := userService.AccessTokenVerify(context.Background(), nil, token)
	if err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.SetHeader("Content-Type", "application/json")
		ctx.BodyWriter().Write([]byte(`{"code":"INVALID_TOKEN","message":"认证凭证无效，请重新登录"}`))
		return
	}
	if u.Disabled {
		ctx.SetStatus(http.StatusForbidden)
		ctx.SetHeader("Content-Type", "application/json")
		ctx.BodyWriter().Write([]byte(`{"code":"ACCOUNT_DISABLED","message":"账号已被禁用"}`))
		return
	}

	ctx = huma.WithValue(ctx, "user", u)
	ctx = huma.WithValue(ctx, "token", token)
	next(ctx)
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
