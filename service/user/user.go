package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"
	"proxy-hub/utils"

	"gorm.io/gorm"
)

type UserListRequest struct {
	Keyword         string
	IncludeDisabled bool
}

const DefaultRootUsername = "root"

func UserCreate(
	ctx context.Context,
	tx model.DBTx,
	username, password, nickname, avatar, brief string,
	beforeInsert func(model.DBTx, *tables.UserTable) error,
) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	var existing tables.UserTable
	if err := tx.Where("username = ?", username).First(&existing).Error; err == nil {
		return nil, ErrUsernameTaken
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	nickname = strings.TrimSpace(nickname)
	avatar = strings.TrimSpace(avatar)
	brief = strings.TrimSpace(brief)

	salt, err := generateSalt()
	if err != nil {
		return nil, err
	}
	hashedPassword, err := hashPassword(password, salt)
	if err != nil {
		return nil, err
	}

	user := &tables.UserTable{
		Nickname: nickname,
		Avatar:   avatar,
		Brief:    brief,
		Username: username,
		Password: hashedPassword,
		Salt:     salt,
		Disabled: false,
	}
	user.ID = utils.NewID()
	user.CreatedAt = time.Now()
	user.UpdatedAt = user.CreatedAt

	if err := tx.Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isUniqueConstraintError(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}

	if beforeInsert != nil {
		if err := beforeInsert(tx, user); err != nil {
			return nil, err
		}
	}

	return user, nil
}

func UserUpdatePassword(ctx context.Context, tx model.DBTx, userID string, newPassword string) error {
	tx = model.GetTx(tx).WithContext(ctx)
	if userID == "" || newPassword == "" {
		return ErrInvalidCredentials
	}

	var user tables.UserTable
	if err := tx.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	salt, err := generateSalt()
	if err != nil {
		return err
	}
	hashedPassword, err := hashPassword(newPassword, salt)
	if err != nil {
		return err
	}

	return tx.Model(&user).
		Updates(map[string]any{
			"password":   hashedPassword,
			"salt":       salt,
			"updated_at": time.Now(),
		}).
		Error
}

func UserAuthenticate(ctx context.Context, tx model.DBTx, username, password string) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var user tables.UserTable
	if err := tx.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !verifyPassword(password, user.Salt, user.Password) {
		return nil, ErrInvalidCredentials
	}

	return &user, nil
}

func UserGet(ctx context.Context, tx model.DBTx, id string) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var user tables.UserTable
	if err := tx.First(&user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func UserGetByUsername(ctx context.Context, tx model.DBTx, username string) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var user tables.UserTable
	if err := tx.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func UserEnsureDefaultRoot(ctx context.Context, tx model.DBTx) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	user, err := UserGetByUsername(ctx, tx, DefaultRootUsername)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}

	password := utils.NewID() + utils.NewID()
	user, err = UserCreate(ctx, tx, DefaultRootUsername, password, "root", "", "内置默认账号", nil)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrUsernameTaken) {
		return nil, err
	}

	user, err = UserGetByUsername(ctx, tx, DefaultRootUsername)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func UserUpdateInfo(ctx context.Context, tx model.DBTx, userID string, nickname, brief, avatar string) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var user tables.UserTable
	if err := tx.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	updates := map[string]any{
		"updated_at": time.Now(),
	}
	if value := strings.TrimSpace(nickname); value != "" {
		updates["nickname"] = value
	}
	if value := strings.TrimSpace(avatar); value != "" {
		updates["avatar"] = value
	}
	if value := strings.TrimSpace(brief); value != "" {
		updates["brief"] = value
	}

	if len(updates) > 1 {
		if err := tx.Model(&user).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return UserGet(ctx, tx, userID)
}

func UserDelete(ctx context.Context, tx model.DBTx, userID string) error {
	tx = model.GetTx(tx).WithContext(ctx)

	var user tables.UserTable
	if err := tx.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	if err := tx.Delete(&user).Error; err != nil {
		return err
	}

	return AccessTokenDeleteAllByUserID(ctx, tx, userID)
}

func UserDisable(ctx context.Context, tx model.DBTx, userID string, disabled bool) (*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)

	var user tables.UserTable
	if err := tx.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	if err := tx.Model(&user).
		Updates(map[string]any{
			"disabled":   disabled,
			"updated_at": time.Now(),
		}).
		Error; err != nil {
		return nil, err
	}

	return UserGet(ctx, tx, userID)
}

func UserList(ctx context.Context, tx model.DBTx, req *UserListRequest, page, size int) ([]*tables.UserTable, int64, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	if req == nil {
		req = &UserListRequest{}
	}

	offset := (page - 1) * size
	keyword := strings.TrimSpace(req.Keyword)

	query := tx.Model(&tables.UserTable{})
	if keyword != "" {
		pattern := keyword + "%"
		query = query.Where("(COALESCE(nickname, '') LIKE ? OR username LIKE ?)", pattern, pattern)
	}
	if !req.IncludeDisabled {
		query = query.Where("disabled = ?", false)
	}

	var count int64
	if err := query.Session(&gorm.Session{}).Count(&count).Error; err != nil {
		return nil, 0, err
	}

	var users []*tables.UserTable
	if err := query.
		// 列表查询不读取 password/salt，避免敏感字段进入内存/日志
		Select("id", "created_at", "updated_at", "deleted_at", "nickname", "avatar", "brief", "username", "disabled").
		Order("created_at DESC").
		Limit(size).
		Offset(offset).
		Find(&users).
		Error; err != nil {
		return nil, 0, err
	}

	return users, count, nil
}

func UserListByIDs(ctx context.Context, tx model.DBTx, ids []string) ([]*tables.UserTable, error) {
	tx = model.GetTx(tx).WithContext(ctx)
	if len(ids) == 0 {
		return []*tables.UserTable{}, nil
	}

	var users []*tables.UserTable
	if err := tx.
		Select("id", "created_at", "updated_at", "deleted_at", "nickname", "avatar", "brief", "username", "disabled").
		Where("id IN ?", ids).
		Find(&users).
		Error; err != nil {
		return nil, err
	}

	return users, nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}

	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "23505" {
		return true
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unique constraint") ||
		strings.Contains(errMsg, "unique violation") ||
		strings.Contains(errMsg, "duplicate entry") ||
		strings.Contains(errMsg, "duplicate key value")
}
