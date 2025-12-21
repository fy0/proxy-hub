package user

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
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

// Argon2id 参数 (OWASP 推荐)
const (
	argonMemory      = 64 * 1024 // 64 MB
	argonIterations  = 3         // 迭代次数
	argonParallelism = 4         // 并行度
	argonKeyLength   = 32        // 输出密钥长度
)

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

	saltBytes, err := base64.RawStdEncoding.DecodeString(salt)
	if err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), saltBytes, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return base64.RawStdEncoding.EncodeToString(hash), nil
}

func verifyPassword(password, salt, hashedPassword string) bool {
	hash, err := hashPassword(password, salt)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(hashedPassword)) == 1
}
