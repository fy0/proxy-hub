package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// IsUniqueConstraintError 汇总各类 SQLite/驱动层唯一约束冲突的判定方式。
func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	type sqlStateError interface {
		SQLState() string
	}
	var stateErr sqlStateError
	if errors.As(err, &stateErr) && stateErr.SQLState() == "23505" {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "unique violation") ||
		strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "duplicate key")
}
