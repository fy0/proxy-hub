package model

import (
	"context"
	"errors"

	"go-template/utils/model_base"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DBTx = *gorm.DB

var (
	db *gorm.DB

	// ErrDBNotReady indicates the gorm.DB has not been initialized.
	ErrDBNotReady = errors.New("model: db is not initialized")
)

func InitWithDSN(dsn string, logLevel int, autoMigrate bool) error {
	var err error
	db, err = model_base.DBInit(dsn, logger.LogLevel(logLevel))
	if err != nil {
		return err
	}

	DBMigrate(autoMigrate)
	return nil
}

func DBClose() {
	if db != nil {
		model_base.DBClose(db)
	}
	db = nil
}

func GetDB() *gorm.DB {
	return db
}

func GetTx(tx DBTx) DBTx {
	if tx != nil {
		return tx
	}
	if db == nil {
		panic("model: db is not initialized")
	}
	return db
}

func Transaction(ctx context.Context, fn func(tx DBTx) error) error {
	if fn == nil {
		return nil
	}

	if db == nil {
		return ErrDBNotReady
	}

	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}
