package user

import (
	"context"
	"time"

	"go-template/model"
	"go-template/utils"
)

func AccessTokenDeleteAllByUserID(ctx context.Context, q *model.Queries, userID string) error {
	q = model.GetQ(q)
	return q.AccessTokenDeleteAllByUserId(ctx, userID)
}

func AccessTokenGenerate(ctx context.Context, q *model.Queries, userID string) (string, error) {
	return AccessTokenGenerateWithTTL(ctx, q, userID, 15*24*time.Hour)
}

func AccessTokenVerify(ctx context.Context, q *model.Queries, tokenString string) (*model.User, error) {
	result := TokenCheck(tokenString)
	if !result.HashValid {
		return nil, ErrInvalidToken
	}
	if !result.TimeValid {
		return nil, ErrTokenExpired
	}

	q = model.GetQ(q)
	token, err := q.AccessTokenGetById(ctx, result.Token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if token.ExpiredAt.Before(time.Now()) {
		return nil, ErrTokenExpired
	}

	user, err := q.UserGetById(ctx, token.UserId)
	if err != nil {
		return nil, ErrUserNotFound
	}

	return user, nil
}

func AccessTokenRefresh(ctx context.Context, q *model.Queries, tokenID string) (string, error) {
	return AccessTokenRefreshWithTTL(ctx, q, tokenID, 15*24*time.Hour)
}

func AccessTokenGenerateWithTTL(ctx context.Context, q *model.Queries, userID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * 24 * time.Hour
	}

	q = model.GetQ(q)
	expiredAt := time.Now().Add(ttl)
	tokenID := utils.NewID()

	params := &model.AccessTokenCreateParams{
		Id:        tokenID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		UserId:    userID,
		ExpiredAt: expiredAt,
	}

	if _, err := q.AccessTokenCreate(ctx, params); err != nil {
		return "", err
	}

	return TokenSign(tokenID, expiredAt), nil
}

func AccessTokenRefreshWithTTL(ctx context.Context, q *model.Queries, tokenID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * 24 * time.Hour
	}

	q = model.GetQ(q)
	expiredAt := time.Now().Add(ttl)
	params := &model.AccessTokenRefreshParams{
		UpdatedAt: time.Now(),
		ExpiredAt: expiredAt,
		Id:        tokenID,
	}

	err := q.AccessTokenRefresh(ctx, params)
	if err != nil {
		return "", ErrInvalidToken
	}

	return TokenSign(tokenID, expiredAt), nil
}

func AcessTokenDeleteAllByUserID(ctx context.Context, q *model.Queries, userID string) error {
	return AccessTokenDeleteAllByUserID(ctx, q, userID)
}
