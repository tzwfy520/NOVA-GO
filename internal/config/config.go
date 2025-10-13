package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用配置结构
type Config struct {
    Server     ServerConfig     `mapstructure:"server"`
    Collector  CollectorConfig  `mapstructure:"collector"`
    Database   DatabaseConfig   `mapstructure:"database"`
    Storage    StorageConfig    `mapstructure:"storage"`
    SSH        SSHConfig        `mapstructure:"ssh"`
    Log        LogConfig        `mapstructure:"log"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Mode         string        `mapstructure:"mode"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// CollectorConfig 采集器配置
type CollectorConfig struct {
	ID         string   `mapstructure:"id"`
	Type       string   `mapstructure:"type"`
	Version    string   `mapstructure:"version"`
	Tags       []string `mapstructure:"tags"`
	Threads    int      `mapstructure:"threads"`
	Concurrent int      `mapstructure:"concurrent"`
}


// DatabaseConfig 数据库配置
type DatabaseConfig struct {
    SQLite SQLiteConfig `mapstructure:"sqlite"`
}

// SQLiteConfig SQLite配置
type SQLiteConfig struct {
    Path            string        `mapstructure:"path"`
    MaxIdleConns    int           `mapstructure:"max_idle_conns"`
    MaxOpenConns    int           `mapstructure:"max_open_conns"`
    ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// StorageConfig 采集数据存储配置（用于原始与格式化数据）
type StorageConfig struct {
    Minio    MinioConfig    `mapstructure:"minio"`
    Postgres PostgresConfig `mapstructure:"postgres"`
}

// MinioConfig 对象存储配置（原始数据）
type MinioConfig struct {
    Host      string `mapstructure:"host"`
    Port      int    `mapstructure:"port"`
    AccessKey string `mapstructure:"access_key"`
    SecretKey string `mapstructure:"secret_key"`
    Bucket    string `mapstructure:"bucket"`
    Secure    bool   `mapstructure:"secure"`
}

// PostgresConfig 格式化数据存储配置（PostgreSQL）
type PostgresConfig struct {
    Host     string `mapstructure:"host"`
    Port     int    `mapstructure:"port"`
    Username string `mapstructure:"username"`
    Password string `mapstructure:"password"`
    Database string `mapstructure:"database"`
}


// SSHConfig SSH配置
type SSHConfig struct {
	Timeout           time.Duration `mapstructure:"timeout"`
	KeepAliveInterval time.Duration `mapstructure:"keep_alive_interval"`
	MaxSessions       int           `mapstructure:"max_sessions"`
}


// LogConfig 日志配置
type LogConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Output     string `mapstructure:"output"`
	FilePath   string `mapstructure:"file_path"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
	Compress   bool   `mapstructure:"compress"`
}

var globalConfig *Config

// Load 加载配置文件
func Load(configPath string) (*Config, error) {
	viper.SetConfigType("yaml")
	
	// 设置默认值
	setDefaults()
	
	if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		// 默认配置文件路径
		viper.SetConfigName("config")
		viper.AddConfigPath("./configs")
		viper.AddConfigPath("../configs")
		viper.AddConfigPath("../../configs")
	}

	// 设置环境变量前缀
	viper.SetEnvPrefix("SSH_COLLECTOR")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 环境变量替换
	config = replaceEnvVars(config)

	globalConfig = &config
	return &config, nil
}

// setDefaults 设置默认配置值
func setDefaults() {
}

// Get 获取全局配置
func Get() *Config {
	return globalConfig
}

// replaceEnvVars 替换配置中的环境变量
func replaceEnvVars(config Config) Config {
	// 替换采集器ID
	if strings.HasPrefix(config.Collector.ID, "${") && strings.HasSuffix(config.Collector.ID, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(config.Collector.ID, "${"), "}")
		if value := os.Getenv(envVar); value != "" {
			config.Collector.ID = value
		}
	}



    return config
}

// GetServerAddr 获取服务器地址
func (c *Config) GetServerAddr() string {
    return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}