package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
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
		// SQLite 默认对每次写操作开启事务，容易放大锁争用；禁用可降低锁冲突几率
		SkipDefaultTransaction: true,
	}

	// 连接数据库，使用modernc.org/sqlite驱动
	var err error
	// 提高 busy_timeout 到 15000ms，缓解并发写争用
	dsn := cfg.Path + "?_pragma=busy_timeout(15000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
	db, err = gorm.Open(sqlite.Dialector{
		DriverName: "sqlite",
		DSN:        dsn,
	}, gormConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// 获取底层sql.DB对象
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 设置连接池参数（单连接），确保 PRAGMA 在唯一连接上生效，避免锁争用
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// 额外保护：运行期设置 PRAGMA（某些环境 DSN 选项可能未生效）
	_ = db.Exec("PRAGMA journal_mode=WAL;").Error
	_ = db.Exec("PRAGMA synchronous=NORMAL;").Error
	_ = db.Exec("PRAGMA busy_timeout=15000;").Error
	_ = db.Exec("PRAGMA foreign_keys=ON;").Error

	// 自动迁移数据库表
	if err := autoMigrate(); err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}

	logger.Info("SQLite database initialized successfully")
	return nil
}

// autoMigrate 自动迁移数据库表
func autoMigrate() error {
	// 先执行模型迁移，创建/更新表结构与索引
	if err := db.AutoMigrate(
		&model.Task{},
		&model.TaskLog{},
		&model.DeviceInfo{},
		&model.SimCommand{},
		&model.SSHPlatform{},
		// 新增：模拟设置配置表
		&model.SimNamespace{},
		&model.SimDeviceType{},
		&model.SimDeviceName{},
		// 新增：按命名空间与设备的模拟命令
		&model.SimDeviceCommand{},
		// 新增：设备类型管理表
		&model.DeviceType{},
		// 新增：采集设置表（保存快速采集的重试与超时）
		&model.CollectorSettings{},
	); err != nil {
		return err
	}

	// 兼容旧版本：移除 DeviceInfo.IP 的单列唯一索引，改用 ip+port+username 组合唯一
	_ = db.Migrator().DropIndex(&model.DeviceInfo{}, "ip")
	// 多尝试几种可能的索引命名，确保旧唯一索引被移除
	_ = db.Exec("DROP INDEX IF EXISTS idx_device_info_ip;").Error
	_ = db.Exec("DROP INDEX IF EXISTS device_info_ip;").Error
	_ = db.Exec("DROP INDEX IF EXISTS uix_device_info_ip;").Error

	return nil
}

// GetDB 获取数据库实例
func GetDB() *gorm.DB {
	return db
}

// IsBusyError 判断是否为 SQLite 并发锁相关错误
func IsBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// 现代驱动错误文案包含以下几类
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "cannot start a transaction within a transaction")
}

// WithRetry 在检测到并发锁错误时进行短暂重试，提升健壮性
func WithRetry(fn func(*gorm.DB) error, attempts int, sleep time.Duration) error {
	if attempts < 1 {
		attempts = 1
	}
	if sleep <= 0 {
		sleep = 50 * time.Millisecond
	}
	var err error
	for i := 0; i < attempts; i++ {
		err = fn(db)
		if err == nil {
			return nil
		}
		if !IsBusyError(err) {
			return err
		}
		// 发生并发写锁竞争，短暂等待重试
		time.Sleep(sleep)
		// 轻微指数退避
		if sleep < 500*time.Millisecond {
			sleep *= 2
		}
	}
	return err
}

// Transaction 执行事务
func Transaction(fn func(*gorm.DB) error) error {
	return db.Transaction(fn)
}

// TransactionWithRetry 在事务级别检测并发锁错误并重试，避免长时间持有锁
func TransactionWithRetry(fn func(*gorm.DB) error, attempts int, sleep time.Duration) error {
	if attempts < 1 {
		attempts = 1
	}
	if sleep <= 0 {
		sleep = 50 * time.Millisecond
	}
	var err error
	for i := 0; i < attempts; i++ {
		// 使用短事务，避免在失败后长时间持锁
		err = db.Transaction(fn)
		if err == nil {
			return nil
		}
		if !IsBusyError(err) {
			return err
		}
		// Busy：退避后重试
		time.Sleep(sleep)
		if sleep < 500*time.Millisecond {
			sleep *= 2
		}
	}
	return err
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
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration":        stats.WaitDuration,
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}
