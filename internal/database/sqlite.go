package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"gorm.io/driver/sqlite"
	_ "modernc.org/sqlite"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var db *gorm.DB

// InitSQLite 初始化SQLite数据库
func InitSQLite(cfg config.SQLiteConfig) error {
	// 确保数据库目录存在
	dbDir := filepath.Dir(cfg.Path)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	// 配置GORM日志
	gormConfig := &gorm.Config{
		Logger: gormLogger.New(
			logger.GetLogger(),
			gormLogger.Config{
				SlowThreshold:             time.Second,
				LogLevel:                  gormLogger.Info,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		),
	}

	// 连接数据库，使用modernc.org/sqlite驱动
	var err error
	db, err = gorm.Open(sqlite.Dialector{
		DriverName: "sqlite",
		DSN:        cfg.Path,
	}, gormConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 获取底层sql.DB对象
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 设置连接池参数
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// 自动迁移数据库表
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}

	logger.Info("SQLite database initialized successfully")
	return nil
}

// autoMigrate 自动迁移数据库表
func autoMigrate() error {
    return db.AutoMigrate(
        &model.Task{},
        &model.TaskLog{},
        &model.DeviceInfo{},
    )
}

// GetDB 获取数据库实例
func GetDB() *gorm.DB {
	return db
}

// Close 关闭数据库连接
func Close() error {
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// Health 检查数据库健康状态
func Health() error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	return sqlDB.Ping()
}

// Transaction 执行事务
func Transaction(fn func(*gorm.DB) error) error {
	return db.Transaction(fn)
}

// GetStats 获取数据库统计信息
func GetStats() map[string]interface{} {
	if db == nil {
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil
	}

	stats := sqlDB.Stats()
	return map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":              stats.InUse,
		"idle":                stats.Idle,
		"wait_count":          stats.WaitCount,
		"wait_duration":       stats.WaitDuration,
		"max_idle_closed":     stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}