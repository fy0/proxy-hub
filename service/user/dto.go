package user

import (
	"strings"
	"time"

	"proxy-hub/model/tables"

	"gorm.io/gorm"
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

func ToUserDTO(user *tables.UserTable) *UserDTO {
	if user == nil {
		return nil
	}
	return &UserDTO{
		Id:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		DeletedAt: deletedAtToPtr(user.DeletedAt),
		Nickname:  stringToPtr(user.Nickname),
		Avatar:    stringToPtr(user.Avatar),
		Brief:     stringToPtr(user.Brief),
		Username:  user.Username,
		Disabled:  user.Disabled,
	}
}

func ToUserDTOs(users []*tables.UserTable) []*UserDTO {
	if len(users) == 0 {
		return []*UserDTO{}
	}
	result := make([]*UserDTO, len(users))
	for i, user := range users {
		result[i] = ToUserDTO(user)
	}
	return result
}

func stringToPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func deletedAtToPtr(value gorm.DeletedAt) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}
