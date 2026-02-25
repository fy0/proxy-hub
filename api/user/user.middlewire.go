package user

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	userService "go-template/service/user"
)

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
