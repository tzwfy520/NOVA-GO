package logger

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

var log *logrus.Logger
var mu sync.RWMutex

// Config 日志配置
type Config struct {
	Level      string `json:"level"`
	Format     string `json:"format"`
	Output     string `json:"output"`
	FilePath   string `json:"file_path"`
	MaxSize    int    `json:"max_size"`
	MaxBackups int    `json:"max_backups"`
	MaxAge     int    `json:"max_age"`
	Compress   bool   `json:"compress"`
}

// Init 初始化日志
func Init(config Config) error {
	mu.Lock()
	defer mu.Unlock()

	if log == nil {
		log = logrus.New()
	}

	// 设置日志级别
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	// 设置日志格式
	if config.Format == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat:   "2006-01-02 15:04:05",
			DisableHTMLEscape: true, // 禁用HTML转义，正确显示<>等字符
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
	}

	// 设置输出
	var writers []io.Writer

	if config.Output == "console" || config.Output == "both" {
		writers = append(writers, os.Stdout)
	}

	if config.Output == "file" || config.Output == "both" {
		// 确保日志目录存在
		if err := os.MkdirAll(filepath.Dir(config.FilePath), 0755); err != nil {
			return err
		}

		fileWriter := &lumberjack.Logger{
			Filename:   config.FilePath,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}
		writers = append(writers, fileWriter)
	}

	if len(writers) > 0 {
		log.SetOutput(io.MultiWriter(writers...))
	}

	return nil
}

// GetLogger 获取日志实例
func GetLogger() *logrus.Logger {
	mu.RLock()
	l := log
	mu.RUnlock()
	if l != nil {
		return l
	}

	mu.Lock()
	defer mu.Unlock()
	if log == nil {
		log = logrus.New()
	}
	return log
}

func asFields(args []interface{}) (string, logrus.Fields, bool) {
	if len(args) < 3 {
		return "", nil, false
	}
	msg, ok := args[0].(string)
	if !ok || msg == "" {
		return "", nil, false
	}
	rest := args[1:]
	if len(rest)%2 != 0 {
		return "", nil, false
	}
	fields := logrus.Fields{}
	for i := 0; i < len(rest); i += 2 {
		k, ok := rest[i].(string)
		if !ok || k == "" {
			return "", nil, false
		}
		fields[k] = rest[i+1]
	}
	return msg, fields, true
}

// Debug 调试日志
func Debug(args ...interface{}) {
	if msg, fields, ok := asFields(args); ok {
		GetLogger().WithFields(fields).Debug(msg)
		return
	}
	GetLogger().Debug(args...)
}

// Debugf 格式化调试日志
func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

// Info 信息日志
func Info(args ...interface{}) {
	if msg, fields, ok := asFields(args); ok {
		GetLogger().WithFields(fields).Info(msg)
		return
	}
	GetLogger().Info(args...)
}

// Infof 格式化信息日志
func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

// Warn 警告日志
func Warn(args ...interface{}) {
	if msg, fields, ok := asFields(args); ok {
		GetLogger().WithFields(fields).Warn(msg)
		return
	}
	GetLogger().Warn(args...)
}

// Warnf 格式化警告日志
func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

// Error 错误日志
func Error(args ...interface{}) {
	if msg, fields, ok := asFields(args); ok {
		GetLogger().WithFields(fields).Error(msg)
		return
	}
	GetLogger().Error(args...)
}

// Errorf 格式化错误日志
func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

// Fatal 致命错误日志
func Fatal(args ...interface{}) {
	if msg, fields, ok := asFields(args); ok {
		GetLogger().WithFields(fields).Fatal(msg)
		return
	}
	GetLogger().Fatal(args...)
}

// Fatalf 格式化致命错误日志
func Fatalf(format string, args ...interface{}) {
	GetLogger().Fatalf(format, args...)
}

// WithField 添加字段
func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

// WithFields 添加多个字段
func WithFields(fields logrus.Fields) *logrus.Entry {
	return GetLogger().WithFields(fields)
}
