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
	Controller ControllerConfig `mapstructure:"controller"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Redis      RedisConfig      `mapstructure:"redis"`
	SSH        SSHConfig        `mapstructure:"ssh"`
	XXLJob     XXLJobConfig     `mapstructure:"xxljob"`
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

// ControllerConfig 控制器配置
type ControllerConfig struct {
	Host              string        `mapstructure:"host"`
	Port              int           `mapstructure:"port"`
	RegisterRetry     int           `mapstructure:"register_retry"`
	RegisterInterval  time.Duration `mapstructure:"register_interval"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `mapstructure:"heartbeat_timeout"`
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

// RedisConfig Redis配置
type RedisConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// SSHConfig SSH配置
type SSHConfig struct {
	Timeout           time.Duration `mapstructure:"timeout"`
	KeepAliveInterval time.Duration `mapstructure:"keep_alive_interval"`
	MaxSessions       int           `mapstructure:"max_sessions"`
}

// XXLJobConfig XXL-Job配置
type XXLJobConfig struct {
	AdminAddresses    string `mapstructure:"admin_addresses"`
	AccessToken       string `mapstructure:"access_token"`
	AppName           string `mapstructure:"app_name"`
	Address           string `mapstructure:"address"`
	IP                string `mapstructure:"ip"`
	Port              int    `mapstructure:"port"`
	LogPath           string `mapstructure:"log_path"`
	LogRetentionDays  int    `mapstructure:"log_retention_days"`
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
	// Redis默认值设置为空，表示不使用Redis
	viper.SetDefault("redis.host", "")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)
	viper.SetDefault("redis.pool_size", 10)
	viper.SetDefault("redis.min_idle_conns", 5)
	viper.SetDefault("redis.dial_timeout", "5s")
	viper.SetDefault("redis.read_timeout", "3s")
	viper.SetDefault("redis.write_timeout", "3s")
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

	// 替换控制器主机
	if strings.HasPrefix(config.Controller.Host, "${") && strings.HasSuffix(config.Controller.Host, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(config.Controller.Host, "${"), "}")
		if value := os.Getenv(envVar); value != "" {
			config.Controller.Host = value
		}
	}

	// 替换Redis配置
	if strings.HasPrefix(config.Redis.Host, "${") && strings.HasSuffix(config.Redis.Host, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(config.Redis.Host, "${"), "}")
		if value := os.Getenv(envVar); value != "" {
			config.Redis.Host = value
		}
	}

	if strings.HasPrefix(config.Redis.Password, "${") && strings.HasSuffix(config.Redis.Password, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(config.Redis.Password, "${"), "}")
		if value := os.Getenv(envVar); value != "" {
			config.Redis.Password = value
		}
	}

	// 替换XXL-Job配置
	if strings.HasPrefix(config.XXLJob.AdminAddresses, "${") && strings.HasSuffix(config.XXLJob.AdminAddresses, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(config.XXLJob.AdminAddresses, "${"), "}")
		if value := os.Getenv(envVar); value != "" {
			config.XXLJob.AdminAddresses = value
		}
	}

	if strings.HasPrefix(config.XXLJob.AccessToken, "${") && strings.HasSuffix(config.XXLJob.AccessToken, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(config.XXLJob.AccessToken, "${"), "}")
		if value := os.Getenv(envVar); value != "" {
			config.XXLJob.AccessToken = value
		}
	}

	return config
}

// GetServerAddr 获取服务器地址
func (c *Config) GetServerAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// GetControllerAddr 获取控制器地址
func (c *Config) GetControllerAddr() string {
	return fmt.Sprintf("%s:%d", c.Controller.Host, c.Controller.Port)
}

// GetRedisAddr 获取Redis地址
func (c *Config) GetRedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Redis.Host, c.Redis.Port)
}