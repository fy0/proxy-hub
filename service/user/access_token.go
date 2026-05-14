package user

import (
	"context"
	"errors"
	"time"

	"proxy-hub/model"
	"proxy-hub/model/tables"

	"gorm.io/gorm"
)

func AccessTokenDeleteAllByUserID(ctx context.Context, tx model.DBTx, userID string) error {
	tx = model.GetTx(tx).WithContext(ctx)
	return tx.Unscoped().
		Where("user_id = ?", userID).
		Delete(&tables.UserAccessTokenTable{}).
		Error
}

func AccessTokenGenerate(ctx context.Context, tx model.DBTx, userID string) (string, error) {
	return AccessTokenGenerateWithTTL(ctx, tx, userID, 15*24*time.Hour)
}

func AccessTokenVerify(ctx context.Context, tx model.DBTx, tokenString string) (*tables.UserTable, error) {
	result := TokenCheck(tokenString)
	if !result.HashValid {
		return nil, ErrInvalidToken
	}
	if !result.TimeValid {
		return nil, ErrTokenExpired
	}

	tx = model.GetTx(tx).WithContext(ctx)

	var token tables.UserAccessTokenTable
	if err := tx.First(&token, "id = ?", result.Token).Error; err != nil {
		return nil, ErrInvalidToken
	}
	if token.ExpiredAt.Before(time.Now()) {
		return nil, ErrTokenExpired
	}

	var user tables.UserTable
	if err := tx.First(&user, "id = ?", token.UserID).Error; err != nil {
		return nil, ErrUserNotFound
	}

	return &user, nil
}

func AccessTokenRefresh(ctx context.Context, tx model.DBTx, tokenID string) (string, error) {
	return AccessTokenRefreshWithTTL(ctx, tx, tokenID, 15*24*time.Hour)
}

func AccessTokenGenerateWithTTL(ctx context.Context, tx model.DBTx, userID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * 24 * time.Hour
	}

	tx = model.GetTx(tx).WithContext(ctx)
	expiredAt := time.Now().Add(ttl)

	token := &tables.UserAccessTokenTable{
		UserID:    userID,
		ExpiredAt: expiredAt,
	}
	token.Init()

	if err := tx.Create(token).Error; err != nil {
		return "", err
	}

	return TokenSign(token.ID, expiredAt), nil
}

func AccessTokenRefreshWithTTL(ctx context.Context, tx model.DBTx, tokenID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * 24 * time.Hour
	}

	tx = model.GetTx(tx).WithContext(ctx)
	expiredAt := time.Now().Add(ttl)

	result := tx.Model(&tables.UserAccessTokenTable{}).
		Where("id = ?", tokenID).
		Updates(map[string]any{
			"expired_at": expiredAt,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", ErrInvalidToken
		}
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", ErrInvalidToken
	}

	return TokenSign(tokenID, expiredAt), nil
}

func AcessTokenDeleteAllByUserID(ctx context.Context, tx model.DBTx, userID string) error {
	return AccessTokenDeleteAllByUserID(ctx, tx, userID)
}
