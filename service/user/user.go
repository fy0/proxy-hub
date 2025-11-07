package user

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"go-template/model"
	"go-template/utils"
)

type UserListRequest struct {
	Keyword         string
	IncludeDisabled bool
}

func UserCreate(ctx context.Context, q *model.Queries, username, password, nickname, avatar, brief string, beforeInsert func(*model.Queries, *model.User) error) (*model.User, error) {
	q = model.GetQ(q)

	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	if existing, err := q.UserGetByUsername(ctx, username); err == nil && existing != nil {
		return nil, ErrUsernameTaken
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
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

	now := time.Now()
	params := &model.UserCreateParams{
		Id:        utils.NewID(),
		CreatedAt: now,
		UpdatedAt: now,
		Nickname:  makeNullString(nickname),
		Avatar:    makeNullString(avatar),
		Brief:     makeNullString(brief),
		Username:  username,
		Password:  hashedPassword,
		Salt:      salt,
		Disabled:  false,
	}

	user, err := q.UserCreate(ctx, params)
	if err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}

	if beforeInsert != nil {
		if err := beforeInsert(q, user); err != nil {
			return nil, err
		}
	}

	return user, nil
}

func UserUpdatePassword(ctx context.Context, q *model.Queries, userID string, newPassword string) error {
	q = model.GetQ(q)
	if userID == "" || newPassword == "" {
		return ErrInvalidCredentials
	}

	if _, err := q.UserGetById(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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

	params := &model.UserUpdatePasswordParams{
		UpdatedAt: time.Now(),
		Password:  hashedPassword,
		Salt:      salt,
		Id:        userID,
	}

	return q.UserUpdatePassword(ctx, params)
}

func UserAuthenticate(ctx context.Context, q *model.Queries, username, password string) (*model.User, error) {
	q = model.GetQ(q)
	user, err := q.UserGetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	hashed, err := hashPassword(password, user.Salt)
	if err != nil {
		return nil, err
	}
	if hashed != user.Password {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

func UserGet(ctx context.Context, q *model.Queries, id string) (*model.User, error) {
	q = model.GetQ(q)
	user, err := q.UserGetById(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

func UserGetByUsername(ctx context.Context, q *model.Queries, username string) (*model.User, error) {
	q = model.GetQ(q)
	user, err := q.UserGetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return user, nil
}

func UserUpdateInfo(ctx context.Context, q *model.Queries, userID string, nickname, brief, avatar string) (*model.User, error) {
	q = model.GetQ(q)
	if _, err := q.UserGetById(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	params := &model.UserUpdateInfoParams{
		UpdatedAt: time.Now(),
		Nickname:  makeNullString(nickname),
		Avatar:    makeNullString(avatar),
		Brief:     makeNullString(brief),
		Id:        userID,
	}

	return q.UserUpdateInfo(ctx, params)
}

func UserDelete(ctx context.Context, q *model.Queries, userID string) error {
	q = model.GetQ(q)
	if _, err := q.UserGetById(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}

	deletedAt := time.Now()
	params := &model.UserDeleteParams{
		DeletedAt: &deletedAt,
		UpdatedAt: deletedAt,
		Id:        userID,
	}

	if err := q.UserDelete(ctx, params); err != nil {
		return err
	}

	return AccessTokenDeleteAllByUserID(ctx, q, userID)
}

func UserDisable(ctx context.Context, q *model.Queries, userID string, disabled bool) (*model.User, error) {
	q = model.GetQ(q)
	if _, err := q.UserGetById(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	params := &model.UserDisableParams{
		UpdatedAt: time.Now(),
		Disabled:  disabled,
		Id:        userID,
	}

	if err := q.UserDisable(ctx, params); err != nil {
		return nil, err
	}

	return q.UserGetById(ctx, userID)
}

func UserList(ctx context.Context, q *model.Queries, req *UserListRequest, page, size int) ([]*model.User, int64, error) {
	q = model.GetQ(q)
	if req == nil {
		req = &UserListRequest{}
	}

	offset := int64((page - 1) * size)

	keyword := ""
	if trimmed := strings.TrimSpace(req.Keyword); trimmed != "" {
		keyword = trimmed + "%"
	}

	count, err := q.UserListCount(ctx, &model.UserListCountParams{
		Keyword:         keyword,
		IncludeDisabled: req.IncludeDisabled,
	})
	if err != nil {
		return nil, 0, err
	}

	rows, err := q.UserList(ctx, &model.UserListParams{
		Keyword:         keyword,
		IncludeDisabled: req.IncludeDisabled,
		Offset:          offset,
		Limit:           int64(size),
	})
	if err != nil {
		return nil, 0, err
	}

	users := make([]*model.User, len(rows))
	for i, row := range rows {
		users[i] = &model.User{
			Id:        row.Id,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
			DeletedAt: row.DeletedAt,
			Nickname:  row.Nickname,
			Avatar:    row.Avatar,
			Brief:     row.Brief,
			Username:  row.Username,
			Password:  "",
			Salt:      "",
			Disabled:  row.Disabled,
		}
	}

	return users, count, nil
}

func UserListByIDs(ctx context.Context, q *model.Queries, ids []string) ([]*model.User, error) {
	q = model.GetQ(q)
	if len(ids) == 0 {
		return []*model.User{}, nil
	}

	users := make([]*model.User, 0, len(ids))
	for _, id := range ids {
		user, err := q.UserGetById(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func makeNullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
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
