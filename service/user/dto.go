package user

import (
	"database/sql"
	"time"

	"go-template/model"
)

type UserDTO struct {
	Id        string     `json:"id"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	Nickname  *string    `json:"nickname,omitempty"`
	Avatar    *string    `json:"avatar,omitempty"`
	Brief     *string    `json:"brief,omitempty"`
	Username  string     `json:"username"`
	Disabled  bool       `json:"disabled"`
}

func ToUserDTO(user *model.User) *UserDTO {
	if user == nil {
		return nil
	}
	return &UserDTO{
		Id:        user.Id,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		DeletedAt: user.DeletedAt,
		Nickname:  nullStringToPtr(user.Nickname),
		Avatar:    nullStringToPtr(user.Avatar),
		Brief:     nullStringToPtr(user.Brief),
		Username:  user.Username,
		Disabled:  user.Disabled,
	}
}

func ToUserDTOs(users []*model.User) []*UserDTO {
	if len(users) == 0 {
		return []*UserDTO{}
	}
	result := make([]*UserDTO, len(users))
	for i, user := range users {
		result[i] = ToUserDTO(user)
	}
	return result
}

func nullStringToPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	value := v.String
	return &value
}
