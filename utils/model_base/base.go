package model_base

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"proxy-hub/utils"

	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/ncruces/go-sqlite3/gormlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type BaseModel struct {
	ID        uint64         `gorm:"primary_key;autoIncrement" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deletedAt"`
}

type StringPKBaseModel struct {
	ID        string         `gorm:"primary_key" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deletedAt"`
}

func (m *StringPKBaseModel) Init() {
	id := utils.NewID()
	m.ID = id
	// CreatedAt 和 UpdatedAt 会在数据库层自动维护，这里预先赋值便于在入库前取值
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
}

func (m *StringPKBaseModel) BeforeCreate(tx *gorm.DB) error {
	// 为避免忘记初始化，写入前兜底生成 ID
	if m.ID == "" {
		m.Init()
	}
	return nil
}

func DBInit(dsn string, logLevel logger.LogLevel) (*gorm.DB, error) {
	var dialector gorm.Dialector
	isSQLite := false
	isSQLiteMemory := false

	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		dialector = postgres.Open(dsn)
	case strings.HasPrefix(dsn, "mysql://") || strings.Contains(dsn, "@tcp("):
		dialector = mysql.Open(strings.TrimPrefix(dsn, "mysql://"))
	case strings.HasSuffix(dsn, ".db") || strings.HasPrefix(dsn, "file:") || strings.HasPrefix(dsn, ":memory:"):
		dialector = gormlite.Open(dsn)
		isSQLite = true
		isSQLiteMemory = isSQLiteMemoryDSN(dsn)
	default:
		return nil, fmt.Errorf("无法识别的数据库类型: %s", dsn)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		TranslateError: true,
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				IgnoreRecordNotFoundError: true,
				LogLevel:                  logLevel,
			},
		),
	})
	if err != nil {
		return nil, err
	}

	// SQLite 模式下开启 WAL 提升并发性能；内存模式仅保留单连接，避免多连接导致表/数据丢失。
	if isSQLite {
		if sqlDB, err := db.DB(); err == nil {
			if isSQLiteMemory {
				sqlDB.SetMaxOpenConns(1)
				sqlDB.SetMaxIdleConns(1)
			}
		}
		if !isSQLiteMemory {
			_ = db.Exec("PRAGMA journal_mode=WAL").Error
		}
	}

	return db, nil
}

func isSQLiteMemoryDSN(dsn string) bool {
	if strings.HasPrefix(dsn, ":memory:") {
		return true
	}

	lower := strings.ToLower(dsn)
	return strings.HasPrefix(lower, "file::memory:") || strings.Contains(lower, "mode=memory")
}

func DBClose(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		panic("关闭数据库失败")
	}
	_ = sqlDB.Close()
}

func FlushWAL(db *gorm.DB) {
	if db == nil || db.Dialector == nil {
		return
	}
	if db.Dialector.Name() != "sqlite" {
		return
	}

	_ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
	_ = db.Exec("PRAGMA shrink_memory").Error
}
