package user

import "proxy-hub/service/user"

type UserSignupRequest struct {
	Username string `json:"username" validate:"required,min=2,max=50"`
	Password string `json:"password" validate:"required,min=3,max=100"`
	Nickname string `json:"nickname" validate:"required,min=1,max=50,no_spaces"`
	Avatar   string `json:"avatar,omitempty" validate:"omitempty"`
	Brief    string `json:"brief,omitempty" validate:"omitempty,max=200"`
}

type UserSigninRequest struct {
	Username string `json:"username" validate:"required,min=2,max=50"`
	Password string `json:"password" validate:"required,min=3,max=100"`
}

type UserChangePasswordRequest struct {
	Password    string `json:"password" validate:"required,min=3,max=100"`
	PasswordNew string `json:"passwordNew" validate:"required,min=3,max=100"`
}

type UserInfoUpdateRequest struct {
	Nickname string `json:"nickname,omitempty" validate:"omitempty,min=1,max=50,no_spaces"`
	Avatar   string `json:"avatar,omitempty" validate:"omitempty"`
	Brief    string `json:"brief,omitempty" validate:"omitempty,max=200"`
}

type UserResponse struct {
	Item *user.UserDTO `json:"item"`
}

type AuthResponse struct {
	Message string `json:"message"`
	Token   string `json:"token"`
}

type UserInfoResponse struct {
	User *user.UserDTO `json:"user"`
}

type UserListQuery struct {
	Page            int    `query:"page" validate:"omitempty,min=1"`
	Size            int    `query:"size" validate:"omitempty,min=1,max=100"`
	Keyword         string `query:"keyword" validate:"omitempty"`
	IncludeDisabled bool   `query:"includeDisabled"`
}

type UserListResponse struct {
	Items []*user.UserDTO `json:"items"`
	Total int64           `json:"total"`
	Page  int             `json:"page"`
	Size  int             `json:"size"`
}
