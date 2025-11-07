package user

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2s"
)

type TokenResult struct {
	Token     string
	HashValid bool
	TimeValid bool
}

func TokenSign(tokenID string, expiredAt time.Time) string {
	return fmt.Sprintf("%s:%d", tokenID, expiredAt.Unix())
}

func TokenCheck(tokenString string) *TokenResult {
	parts := strings.Split(tokenString, ":")
	if len(parts) != 2 {
		return &TokenResult{HashValid: false, TimeValid: false}
	}

	tokenID := parts[0]
	expiredAtUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return &TokenResult{HashValid: false, TimeValid: false}
	}

	expiredAt := time.Unix(expiredAtUnix, 0)
	now := time.Now()

	return &TokenResult{
		Token:     tokenID,
		HashValid: true,
		TimeValid: now.Before(expiredAt),
	}
}

func generateSalt() (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(salt), nil
}

func hashPassword(password string, salt string) (string, error) {
	if password == "" || salt == "" {
		return "", ErrInvalidCredentials
	}

	hashBytes := blake2s.Sum256([]byte(password + salt))
	return base64.RawStdEncoding.EncodeToString(hashBytes[:]), nil
}
