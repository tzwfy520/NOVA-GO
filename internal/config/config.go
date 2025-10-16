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
	Server    ServerConfig    `mapstructure:"server"`
	Collector CollectorConfig `mapstructure:"collector"`
	Database  DatabaseConfig  `mapstructure:"database"`
	Storage   StorageConfig   `mapstructure:"storage"`
	SSH       SSHConfig       `mapstructure:"ssh"`
	Log       LogConfig       `mapstructure:"log"`
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
    // OutputFilter 用于原始输出的行过滤（移除分页提示等）
    OutputFilter OutputFilterConfig `mapstructure:"output_filter"`
    // Interact 交互配置：自动交互参数对与错误提示匹配
    Interact InteractConfig `mapstructure:"interact"`
    // DeviceDefaults 按设备平台加载的交互/适配参数（提示符、分页、enable、自动交互）
    DeviceDefaults map[string]PlatformDefaultsConfig `mapstructure:"device_defaults"`
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
	// 默认输出过滤规则：大小写不敏感，去除首尾空格
	viper.SetDefault("collector.output_filter.case_insensitive", true)
	viper.SetDefault("collector.output_filter.trim_space", true)
	// 默认前缀匹配：H3C/Huawei 页提示与纯 more 行
	viper.SetDefault("collector.output_filter.prefixes", []string{"---- More ----", "more"})
	// 默认包含匹配：Cisco --more-- 提示
	viper.SetDefault("collector.output_filter.contains", []string{"--more--"})

    // 默认交互配置
    viper.SetDefault("collector.interact.case_insensitive", true)
    viper.SetDefault("collector.interact.trim_space", true)
    // 默认自动交互为空（由各平台插件提供）
    viper.SetDefault("collector.interact.auto_interactions", []map[string]string{})
    // 默认错误提示前缀（可按需调整或清空）
    viper.SetDefault("collector.interact.error_hints", []string{"ERROR:", "invalid parameters detect"})

    // 设备平台默认（可在配置文件中覆盖或新增平台）
    viper.SetDefault("collector.device_defaults", map[string]map[string]interface{}{
        "cisco_ios": {
            "prompt_suffixes":      []string{">", "#"},
            "disable_paging_cmds":  []string{"terminal length 0"},
            "enable_required":       true,
            "enable_cli":            "enable",
            "except_output":         "Password:",
            "skip_delayed_echo":     true,
            "error_hints":           []string{"invalid input detected", "incomplete command", "ambiguous command", "unknown command", "invalid autocommand", "line has invalid autocommand"},
            "auto_interactions": []map[string]string{
                {"except_output": "--more--", "command_auto_send": " ",},
                {"except_output": "more", "command_auto_send": " ",},
                {"except_output": "press any key", "command_auto_send": " ",},
                {"except_output": "confirm", "command_auto_send": "y",},
                {"except_output": "[yes/no]", "command_auto_send": "yes",},
            },
        },
        "huawei": {
            "prompt_suffixes":      []string{">", "#", "]"},
            "disable_paging_cmds":  []string{"screen-length disable"},
            "enable_required":       false,
            "skip_delayed_echo":     true,
            "error_hints":           []string{"error:", "unrecognized command"},
            "auto_interactions": []map[string]string{
                {"except_output": "more", "command_auto_send": " ",},
                {"except_output": "press any key", "command_auto_send": " ",},
                {"except_output": "confirm", "command_auto_send": "y",},
            },
        },
        "h3c": {
            "prompt_suffixes":      []string{">", "#", "]"},
            "disable_paging_cmds":  []string{"screen-length disable"},
            "enable_required":       false,
            "skip_delayed_echo":     true,
            "error_hints":           []string{"error:", "unrecognized command"},
            "auto_interactions": []map[string]string{
                {"except_output": "more", "command_auto_send": " ",},
                {"except_output": "press any key", "command_auto_send": " ",},
            },
        },
    })
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

// OutputFilterConfig 定义原始输出的行过滤规则
type OutputFilterConfig struct {
	// Prefixes 前缀匹配：如果行（可选 trim/casefold 后）以这些前缀之一开始，则移除
	Prefixes []string `mapstructure:"prefixes"`
	// Contains 包含匹配：如果行（可选 trim/casefold 后）包含这些子串之一，则移除
	Contains []string `mapstructure:"contains"`
	// CaseInsensitive 是否大小写不敏感匹配（默认 true）
	CaseInsensitive bool `mapstructure:"case_insensitive"`
	// TrimSpace 是否在匹配前对行做 TrimSpace（默认 true）
	TrimSpace bool `mapstructure:"trim_space"`
}

// InteractConfig 交互式会话的配置：自动交互与错误提示
type InteractConfig struct {
	// AutoInteractions 当输出包含 expect_output（大小写可不敏感）时自动发送 auto_send
	AutoInteractions []AutoInteractionConfig `mapstructure:"auto_interactions"`
	// ErrorHints 错误提示前缀列表（匹配到则认为命令可能错误）
	ErrorHints []string `mapstructure:"error_hints"`
	// CaseInsensitive 是否大小写不敏感匹配（默认 true）
	CaseInsensitive bool `mapstructure:"case_insensitive"`
	// TrimSpace 是否在匹配前对行做 TrimSpace（默认 true）
	TrimSpace bool `mapstructure:"trim_space"`
}

// AutoInteractionConfig 配置中的自动交互项
type AutoInteractionConfig struct {
    ExpectOutput string `mapstructure:"except_output"`
    AutoSend     string `mapstructure:"command_auto_send"`
}

// PlatformDefaultsConfig 设备平台默认参数（可在配置文件中扩展）
type PlatformDefaultsConfig struct {
    PromptSuffixes     []string               `mapstructure:"prompt_suffixes"`
    DisablePagingCmds  []string               `mapstructure:"disable_paging_cmds"`
    AutoInteractions   []AutoInteractionConfig `mapstructure:"auto_interactions"`
    ErrorHints         []string               `mapstructure:"error_hints"`
    SkipDelayedEcho    bool                   `mapstructure:"skip_delayed_echo"`
    EnableRequired     bool                   `mapstructure:"enable_required"`
    // 提权设置：当 enable_required 为 true 时，可指定提权命令与密码提示匹配
    // enable_cli 定义提权命令（例如 "enable" 或 Linux 平台的 "sudo -i"）
    // except_output 定义收到哪条输出后自动输入 enable 密码
    EnableCLI          string                 `mapstructure:"enable_cli"`
    EnableExceptOutput string                 `mapstructure:"except_output"`
}
