package user

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"go-template/api/h"
	"go-template/model"
	userService "go-template/service/user"
	"go-template/utils"
)

const (
	ctxUserKey  = "user"
	ctxTokenKey = "token"

	userTag  = "user-用户"
	userPath = "/user"
)

func Register(api huma.API) {
	group := huma.NewGroup(api, userPath)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/signup",
		Summary:     "用户注册",
		OperationID: "user-signup",
		Tags:        []string{userTag},
	}, userSignupHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/signin",
		Summary:     "用户登录",
		OperationID: "user-signin",
		Tags:        []string{userTag},
	}, userSigninHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/change-password",
		Summary:     "修改密码",
		OperationID: "user-change-password",
		Tags:        []string{userTag},
		Middlewares: huma.Middlewares{SignCheckHumaMiddleware},
	}, userChangePasswordHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/info",
		Summary:     "获取当前用户信息",
		OperationID: "user-info",
		Tags:        []string{userTag},
		Middlewares: huma.Middlewares{SignCheckHumaMiddleware},
	}, userInfoHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodPost,
		Path:        "/info-update",
		Summary:     "更新当前用户信息",
		OperationID: "user-info-update",
		Tags:        []string{userTag},
		Middlewares: huma.Middlewares{SignCheckHumaMiddleware},
	}, userInfoUpdateHandler)

	h.HumaRegister(group, huma.Operation{
		Method:      http.MethodGet,
		Path:        "/list",
		Summary:     "用户列表",
		OperationID: "user-list",
		Tags:        []string{userTag},
		Middlewares: huma.Middlewares{SignCheckHumaMiddleware},
	}, userListHandler)
}

type signupInput struct {
	Body UserSignupRequest
}

type signupOutput struct {
	Body UserResponse `json:"body"`
}

func userSignupHandler(ctx context.Context, input *signupInput) (*signupOutput, error) {
	var created *model.User
	err := model.Transaction(ctx, func(q *model.Queries) error {
		user, err := userService.UserCreate(ctx, q,
			input.Body.Username,
			input.Body.Password,
			input.Body.Nickname,
			input.Body.Avatar,
			input.Body.Brief,
			nil,
		)
		if err != nil {
			return err
		}
		created = user
		return nil
	})
	if err != nil {
		return nil, mapError(err)
	}

	return &signupOutput{
		Body: UserResponse{Item: userService.ToUserDTO(created)},
	}, nil
}

type signinInput struct {
	Body UserSigninRequest
}

type signinOutput struct {
	Body AuthResponse `json:"body"`
}

func userSigninHandler(ctx context.Context, input *signinInput) (*signinOutput, error) {
	q := model.GetQ(nil)
	u, err := userService.UserAuthenticate(ctx, q, input.Body.Username, input.Body.Password)
	if err != nil {
		return nil, mapError(err)
	}

	if err := userService.AccessTokenDeleteAllByUserID(ctx, q, u.Id); err != nil {
		return nil, humanaError(http.StatusInternalServerError, err.Error())
	}
	token, err := userService.AccessTokenGenerate(ctx, q, u.Id)
	if err != nil {
		return nil, humanaError(http.StatusInternalServerError, err.Error())
	}

	return &signinOutput{
		Body: AuthResponse{
			Message: "登录成功",
			Token:   token,
		},
	}, nil
}

type changePasswordInput struct {
	Body UserChangePasswordRequest
}

type changePasswordOutput struct {
	Body map[string]string `json:"body"`
}

func userChangePasswordHandler(ctx context.Context, input *changePasswordInput) (*changePasswordOutput, error) {
	current := ctx.Value(ctxUserKey)
	userInfo, ok := current.(*model.User)
	if !ok || userInfo == nil {
		return nil, humanaError(http.StatusUnauthorized, "未检测到登录状态")
	}

	q := model.GetQ(nil)
	if _, err := userService.UserAuthenticate(ctx, q, userInfo.Username, input.Body.Password); err != nil {
		return nil, mapError(err)
	}
	if err := userService.UserUpdatePassword(ctx, q, userInfo.Id, input.Body.PasswordNew); err != nil {
		return nil, mapError(err)
	}
	if err := userService.AccessTokenDeleteAllByUserID(ctx, q, userInfo.Id); err != nil {
		return nil, humanaError(http.StatusInternalServerError, err.Error())
	}

	return &changePasswordOutput{
		Body: map[string]string{"message": "密码修改成功"},
	}, nil
}

type infoOutput struct {
	Body struct {
		Item UserInfoResponse `json:"item"`
	} `json:"body"`
}

func userInfoHandler(ctx context.Context, _ *struct{}) (*infoOutput, error) {
	current := ctx.Value(ctxUserKey)
	userInfo, ok := current.(*model.User)
	if !ok || userInfo == nil {
		return nil, humanaError(http.StatusUnauthorized, "未检测到登录状态")
	}

	q := model.GetQ(nil)
	refreshed, err := userService.UserGet(ctx, q, userInfo.Id)
	if err != nil {
		return nil, mapError(err)
	}

	return &infoOutput{
		Body: struct {
			Item UserInfoResponse `json:"item"`
		}{
			Item: UserInfoResponse{User: userService.ToUserDTO(refreshed)},
		},
	}, nil
}

type updateInfoInput struct {
	Body UserInfoUpdateRequest
}

type updateInfoOutput struct {
	Body UserResponse `json:"body"`
}

func userInfoUpdateHandler(ctx context.Context, input *updateInfoInput) (*updateInfoOutput, error) {
	current := ctx.Value(ctxUserKey)
	userInfo, ok := current.(*model.User)
	if !ok || userInfo == nil {
		return nil, humanaError(http.StatusUnauthorized, "未检测到登录状态")
	}

	q := model.GetQ(nil)
	updated, err := userService.UserUpdateInfo(ctx, q, userInfo.Id, input.Body.Nickname, input.Body.Brief, input.Body.Avatar)
	if err != nil {
		return nil, mapError(err)
	}

	return &updateInfoOutput{
		Body: UserResponse{Item: userService.ToUserDTO(updated)},
	}, nil
}

type listInput struct {
	Page            int    `query:"page" validate:"omitempty,min=1"`
	Size            int    `query:"size" validate:"omitempty,min=1,max=20"`
	Keyword         string `query:"keyword" validate:"omitempty"`
	IncludeDisabled bool   `query:"includeDisabled"`
}

type listOutput struct {
	Body UserListResponse `json:"body"`
}

func userListHandler(ctx context.Context, input *listInput) (*listOutput, error) {
	page := utils.GetPage(input.Page)
	size := utils.GetPageSize(input.Size, 20)

	req := &userService.UserListRequest{
		Keyword:         input.Keyword,
		IncludeDisabled: input.IncludeDisabled,
	}

	q := model.GetQ(nil)
	users, total, err := userService.UserList(ctx, q, req, page, size)
	if err != nil {
		return nil, mapError(err)
	}

	dtoList := make([]*userService.UserDTO, len(users))
	for i, u := range users {
		dtoList[i] = userService.ToUserDTO(u)
	}

	return &listOutput{
		Body: UserListResponse{
			Items: dtoList,
			Total: total,
			Page:  page,
			Size:  size,
		},
	}, nil
}

func mapError(err error) error {
	switch {
	case errors.Is(err, userService.ErrInvalidCredentials):
		return humanaError(http.StatusBadRequest, "用户名或密码错误")
	case errors.Is(err, userService.ErrUsernameTaken):
		return humanaError(http.StatusConflict, "用户名已存在")
	case errors.Is(err, userService.ErrUserNotFound):
		return humanaError(http.StatusNotFound, "用户不存在")
	case errors.Is(err, userService.ErrInvalidToken):
		return humanaError(http.StatusBadRequest, "认证凭证无效")
	case errors.Is(err, userService.ErrTokenExpired):
		return humanaError(http.StatusBadRequest, "认证凭证已过期")
	default:
		return humanaError(http.StatusInternalServerError, err.Error())
	}
}

func humanaError(code int, message string) error {
	return &apiError{status: code, message: message}
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	return e.message
}

func (e *apiError) HTTPStatus() int {
	return e.status
}
