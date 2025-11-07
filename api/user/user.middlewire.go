package user

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v2"

	"go-template/model"
	userService "go-template/service/user"
)

func getTokenFromFiber(c *fiber.Ctx) string {
	token := c.Get("Authorization")
	if token != "" {
		if after, ok := strings.CutPrefix(token, "Bearer "); ok {
			token = after
		}
		return strings.TrimSpace(token)
	}
	if cookie := c.Cookies("Authorization"); cookie != "" {
		return cookie
	}
	return ""
}

func SignCheckFiberMiddleware(c *fiber.Ctx) error {
	token := getTokenFromFiber(c)
	if token == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"code":    "MISSING_TOKEN",
			"message": "缺少认证凭证",
		})
	}

	q := model.GetQ(nil)
	u, err := userService.AccessTokenVerify(context.Background(), q, token)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"code":    "INVALID_TOKEN",
			"message": "认证凭证无效，请重新登录",
		})
	}
	if u.Disabled {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{
			"code":    "ACCOUNT_DISABLED",
			"message": "账号已被禁用",
		})
	}

	c.Locals("user", u)
	c.Locals("token", token)
	return c.Next()
}

func getTokenFromHuma(ctx huma.Context) string {
	if auth := ctx.Header("Authorization"); auth != "" {
		if after, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		return strings.TrimSpace(auth)
	}
	if cookies := ctx.Header("Cookie"); cookies != "" {
		for _, part := range strings.Split(cookies, ";") {
			part = strings.TrimSpace(part)
			if token, ok := strings.CutPrefix(part, "Authorization="); ok {
				return token
			}
		}
	}
	return ""
}

func SignCheckHumaMiddleware(ctx huma.Context, next func(huma.Context)) {
	token := getTokenFromHuma(ctx)
	if token == "" {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.SetHeader("Content-Type", "application/json")
		ctx.BodyWriter().Write([]byte(`{"code":"MISSING_TOKEN","message":"缺少认证凭证"}`))
		return
	}

	q := model.GetQ(nil)
	u, err := userService.AccessTokenVerify(context.Background(), q, token)
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
