package config

import (
	"fmt"
	"os"
	"strconv"
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
	Backup     BackupConfig     `mapstructure:"backup"`
	DataFormat DataFormatConfig `mapstructure:"data_format"`
	Deploy     DeployConfig     `mapstructure:"deploy"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	Mode         string        `mapstructure:"mode"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	SimulateEnable bool        `mapstructure:"simulate_enable"`
}

// CollectorConfig 采集器配置
type CollectorConfig struct {
	ID         string   `mapstructure:"id"`
	Type       string   `mapstructure:"type"`
	Version    string   `mapstructure:"version"`
	Tags       []string `mapstructure:"tags"`
	Threads    int      `mapstructure:"threads"`
	Concurrent int      `mapstructure:"concurrent"`
	// RetryFlags 默认重试次数：接口未指定时使用
	RetryFlags int `mapstructure:"retry_flags"`
	// ConcurrencyProfile 并发档位：S/M/L/XL（优先级高于 concurrent 数值）
	ConcurrencyProfile string `mapstructure:"concurrency_profile"`
	// ConcurrencyProfiles 并发档位映射：每个档位同时指定并发与线程数
	// 结构示例：{"S":{"concurrent":8,"threads":32}, ...}
	ConcurrencyProfiles map[string]ConcurrencyProfileConfig `mapstructure:"concurrency_profiles"`
	// OutputFilter 用于原始输出的行过滤（移除分页提示等）
	OutputFilter OutputFilterConfig `mapstructure:"output_filter"`
	// Interact 交互配置：自动交互参数对与错误提示匹配
	Interact InteractConfig `mapstructure:"interact"`
	// DeviceDefaults 按设备平台加载的交互/适配参数（提示符、分页、enable、自动交互）
	DeviceDefaults map[string]PlatformDefaultsConfig `mapstructure:"device_defaults"`
}

// ConcurrencyProfileConfig 并发档位配置：并发与线程数
type ConcurrencyProfileConfig struct {
	Concurrent int `mapstructure:"concurrent"`
	Threads    int `mapstructure:"threads"`
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

// DataFormatConfig 格式化数据相关配置
type DataFormatConfig struct {
	// MinioPrefix 用于格式化数据在 MinIO 中的顶层路径（不含 bucket）
	MinioPrefix string `mapstructure:"minio_prefix"`
}

// DeployConfig 部署相关配置
type DeployConfig struct {
	// 部署相关等待时间（毫秒），用于控制前后采集等待与下发后等待
	DeployWaitMS int `mapstructure:"deploy_wait_ms"`
}

// BackupConfig 备份服务配置
type BackupConfig struct {
	// StorageBackend 默认存储后端：local | minio
	StorageBackend string `mapstructure:"storage_backend"`
	// Prefix 顶层保存目录前缀（与请求中的 save_dir 组合）
	Prefix string            `mapstructure:"prefix"`
	Local  LocalBackupConfig `mapstructure:"local"`
	// Aggregate 聚合配置（是否将所有 CLI 输出写入单一文件）
	Aggregate AggregateConfig `mapstructure:"aggregate"`
}

// LocalBackupConfig 本地存储配置
type LocalBackupConfig struct {
	BaseDir        string `mapstructure:"base_dir"`
	Prefix         string `mapstructure:"prefix"`
	MkdirIfMissing bool   `mapstructure:"mkdir_if_missing"`
	Compress       bool   `mapstructure:"compress"`
}

// AggregateConfig 聚合写入配置
type AggregateConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Filename string `mapstructure:"filename"` // 可带扩展名，例如 all_cli.txt
	// AggregateOnly 启用后仅生成聚合文件，跳过逐命令写入
	AggregateOnly bool `mapstructure:"aggregate_only"`
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
	// Timeout 不直接映射顶层 ssh.timeout（避免与嵌套块冲突）；在 Load 中手动填充
	Timeout           time.Duration `mapstructure:"-"`
	ConnectTimeout    time.Duration `mapstructure:"connect_timeout"`
	KeepAliveInterval time.Duration `mapstructure:"keep_alive_interval"`
	CleanupInterval   time.Duration `mapstructure:"cleanup_interval"`
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

	// 兼容旧键名：backup.backup_backend -> backup.storage_backend
	if strings.TrimSpace(config.Backup.StorageBackend) == "" {
		if viper.IsSet("backup.backup_backend") {
			bb := strings.TrimSpace(viper.GetString("backup.backup_backend"))
			if bb != "" {
				config.Backup.StorageBackend = bb
			}
		}
	}

	// 兼容旧顶层键：deploy_wait_ms -> deploy.deploy_wait_ms
	if config.Deploy.DeployWaitMS <= 0 {
		if viper.IsSet("deploy_wait_ms") {
			val := viper.GetInt("deploy_wait_ms")
			if val > 0 {
				config.Deploy.DeployWaitMS = val
			}
		}
	}

	// 兼容新嵌套：ssh.timeout.*（若存在则覆盖旧字段）
	if viper.IsSet("ssh.timeout.timeout_all") {
		to := viper.GetDuration("ssh.timeout.timeout_all")
		if to > 0 {
			config.SSH.Timeout = to
		}
	}
	// 兼容旧顶层：ssh.timeout（若仍为时长字符串则生效；嵌套块不影响）
	if config.SSH.Timeout <= 0 {
		if to := viper.GetDuration("ssh.timeout"); to > 0 {
			config.SSH.Timeout = to
		}
	}
	// 支持拆分的握手超时（dial/auth）；若设置则合并为 ConnectTimeout
	var dialSec, authSec int
	if viper.IsSet("ssh.timeout.dial_timeout") {
		dialSec = viper.GetInt("ssh.timeout.dial_timeout")
	}
	if viper.IsSet("ssh.timeout.auth_timeout") {
		authSec = viper.GetInt("ssh.timeout.auth_timeout")
	}
	if dialSec > 0 || authSec > 0 {
		merged := time.Duration(dialSec+authSec) * time.Second
		config.SSH.ConnectTimeout = merged
	}

	// 环境变量替换
	config = replaceEnvVars(config)

	// 应用并发档位配置（若设置了 concurrency_profile 则覆盖 concurrent 数值）
	applyConcurrencyProfile(&config)

	globalConfig = &config
	return &config, nil
}

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

	// 不预设设备平台默认项：完全由配置文件控制。
	// 若需要兜底，可在配置文件中提供 collector.device_defaults.default 项。
	// 这里不设置 viper 默认，避免内置平台行为。

	// 默认并发档位配置（包含并发与线程数）
	viper.SetDefault("collector.concurrency_profile", "S")
	viper.SetDefault("collector.concurrency_profiles", map[string]map[string]int{
		"S":  {"concurrent": 8, "threads": 32},   // 2c4g
		"M":  {"concurrent": 16, "threads": 64},  // 4c8g
		"L":  {"concurrent": 32, "threads": 128}, // 8c16g
		"XL": {"concurrent": 64, "threads": 256}, // 16c32g
	})
	// 默认重试次数（接口未指定时使用）。若配置文件未设置，则使用 1。
	viper.SetDefault("collector.retry_flags", 1)

	// 备份服务默认配置
	viper.SetDefault("backup.storage_backend", "local")
	// 顶层前缀默认用于在 base_dir 下分组，如 "configs"
	viper.SetDefault("backup.prefix", "configs")
	viper.SetDefault("backup.local.base_dir", "./data/backups")
	// 可选：局部覆盖的前缀，默认空串，最终路径 prefix/local.prefix/save_dir
	viper.SetDefault("backup.local.prefix", "")
	viper.SetDefault("backup.local.mkdir_if_missing", true)
	viper.SetDefault("backup.local.compress", false)
	// 聚合写入默认开启，聚合文件名默认为 all_cli.txt
	viper.SetDefault("backup.aggregate.enabled", true)
	viper.SetDefault("backup.aggregate.filename", "all_cli.txt")
	// 聚合仅写入模式默认关闭（false 表示仍写入逐命令文件）
	viper.SetDefault("backup.aggregate.aggregate_only", false)

	// 格式化数据默认配置
	// 仅定义 MinIO 路径前缀，最终对象路径为 /{minio_prefix}/{save_dir}/{task_id}/...
	viper.SetDefault("data_format.minio_prefix", "data-formats")

	// SSH 超时新默认（替换旧的 connect_timeout 与顶层 timeout）
	// 全局执行窗口（接口未指定时可参考此值）
	viper.SetDefault("ssh.timeout.timeout_all", 60*time.Second)
	// 拨号与握手阶段拆分默认（合并为 ConnectTimeout 使用）
	viper.SetDefault("ssh.timeout.dial_timeout", 2)
	viper.SetDefault("ssh.timeout.auth_timeout", 5)

	// 新增：连接池清理周期默认 30s（可通过 ssh.cleanup_interval 覆盖）
	viper.SetDefault("ssh.cleanup_interval", 30*time.Second)

	// 新增：模拟服务开关默认关闭
	viper.SetDefault("server.simulate_enable", false)

	// 新增：日志默认级别为 info（可通过 log.level 覆盖为 debug/warn/error 等）
	viper.SetDefault("log.level", "info")
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

// applyConcurrencyProfile 根据并发档位设置并发数（覆盖 Collector.Concurrent）
func applyConcurrencyProfile(cfg *Config) {
	prof := strings.TrimSpace(cfg.Collector.ConcurrencyProfile)
	if prof == "" {
		return
	}
	// 兼容大小写与可能的前缀（例如 "Concurrency-S"）
	p := strings.ToUpper(prof)
	if after, ok := strings.CutPrefix(p, "CONCURRENCY-"); ok {
		p = after

	}

	// 获取档位映射（兼容旧格式与新格式）
	mapping := make(map[string]ConcurrencyProfileConfig)
	if len(cfg.Collector.ConcurrencyProfiles) > 0 {
		for k, v := range cfg.Collector.ConcurrencyProfiles {
			mapping[strings.ToUpper(k)] = v
		}
	} else {
		raw := viper.Get("collector.concurrency_profiles")
		switch rm := raw.(type) {
		case map[string]interface{}:
			for k, v := range rm {
				key := strings.ToUpper(k)
				switch vv := v.(type) {
				case int:
					mapping[key] = ConcurrencyProfileConfig{Concurrent: vv}
				case int64:
					mapping[key] = ConcurrencyProfileConfig{Concurrent: int(vv)}
				case float64:
					mapping[key] = ConcurrencyProfileConfig{Concurrent: int(vv)}
				case string:
					if n, err := strconv.Atoi(vv); err == nil && n > 0 {
						mapping[key] = ConcurrencyProfileConfig{Concurrent: n}
					}
				case map[string]interface{}:
					var cp ConcurrencyProfileConfig
					if c, ok := vv["concurrent"]; ok {
						switch cv := c.(type) {
						case int:
							cp.Concurrent = cv
						case int64:
							cp.Concurrent = int(cv)
						case float64:
							cp.Concurrent = int(cv)
						case string:
							if n, err := strconv.Atoi(cv); err == nil {
								cp.Concurrent = n
							}
						}
					}
					if t, ok := vv["threads"]; ok {
						switch tv := t.(type) {
						case int:
							cp.Threads = tv
						case int64:
							cp.Threads = int(tv)
						case float64:
							cp.Threads = int(tv)
						case string:
							if n, err := strconv.Atoi(tv); err == nil {
								cp.Threads = n
							}
						}
					}
					mapping[key] = cp
				}
			}
		}
	}

	if profCfg, ok := mapping[p]; ok {
		if profCfg.Concurrent > 0 {
			cfg.Collector.Concurrent = profCfg.Concurrent
		}
		if profCfg.Threads > 0 {
			cfg.Collector.Threads = profCfg.Threads
		}
	}
}

// GetServerAddr 获取服务器地址
func (c *Config) GetServerAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// OutputFilterConfig 输出过滤器配置
type OutputFilterConfig struct {
	// Prefixes: 移除以这些字符串开头的行（例如分页提示 "---- More ----" 或纯 more 行）
	Prefixes []string `mapstructure:"prefixes"`
	// Contains: 移除包含这些子串的行（例如 Cisco 的 "--more--"）
	Contains []string `mapstructure:"contains"`
	// CaseInsensitive: 忽略大小写匹配（默认启用）
	CaseInsensitive bool `mapstructure:"case_insensitive"`
	// TrimSpace: 过滤后再移除首尾空格（默认启用）
	TrimSpace bool `mapstructure:"trim_space"`
}

// InteractConfig 交互配置（针对设备交互行为的过滤与提示匹配）
type InteractConfig struct {
	// AutoInteractions: 自动交互配置（遇到输出提示时自动发送命令）
	AutoInteractions []AutoInteractionConfig `mapstructure:"auto_interactions"`
	// ErrorHints: 错误提示前缀（命令错误或无效提示）
	ErrorHints []string `mapstructure:"error_hints"`
	// CaseInsensitive: 错误提示匹配忽略大小写（默认启用）
	CaseInsensitive bool `mapstructure:"case_insensitive"`
	// TrimSpace: 匹配前移除首尾空格（默认启用）
	TrimSpace bool `mapstructure:"trim_space"`
}

// AutoInteractionConfig 自动交互项
type AutoInteractionConfig struct {
	ExpectOutput string `mapstructure:"except_output"`
	AutoSend     string `mapstructure:"command_auto_send"`
}

// InteractTimingConfig 平台交互时序与节奏参数（可嵌套于 platform.timeout.interact_timeout）
type InteractTimingConfig struct {
	CommandIntervalMS        int `mapstructure:"command_interval_ms"`
	CommandTimeoutSec        int `mapstructure:"command_timeout_sec"`
	QuietAfterMS             int `mapstructure:"quiet_after_ms"`
	QuietPollIntervalMS      int `mapstructure:"quiet_poll_interval_ms"`
	EnablePasswordFallbackMS int `mapstructure:"enable_password_fallback_ms"`
	PromptInducerIntervalMS  int `mapstructure:"prompt_inducer_interval_ms"`
	PromptInducerMaxCount    int `mapstructure:"prompt_inducer_max_count"`
	ExitPauseMS              int `mapstructure:"exit_pause_ms"`
}

// PlatformTimeoutConfig 平台层 timeout 嵌套块（兼容全局结构）
type PlatformTimeoutConfig struct {
	TimeoutAll     time.Duration       `mapstructure:"timeout_all"`
	DialTimeoutSec int                 `mapstructure:"dial_timeout"`
	AuthTimeoutSec int                 `mapstructure:"auth_timeout"`
	Interact       InteractTimingConfig `mapstructure:"interact_timeout"`
}

// PlatformDefaultsConfig 平台默认交互配置（提示符、分页、enable 与自动交互）
type PlatformDefaultsConfig struct {
    PromptSuffixes    []string                `mapstructure:"prompt_suffixes"`
    DisablePagingCmds []string                `mapstructure:"disable_paging_cmds"`
    AutoInteractions  []AutoInteractionConfig `mapstructure:"auto_interactions"`
    ErrorHints        []string                `mapstructure:"error_hints"`
    SkipDelayedEcho   bool                    `mapstructure:"skip_delayed_echo"`
    EnableRequired    bool                    `mapstructure:"enable_required"`
    // OutputFilter：平台级输出过滤（与全局 collector.output_filter 合并或覆盖）
    OutputFilter OutputFilterConfig `mapstructure:"output_filter"`
    // Interact: 平台级交互配置（错误提示匹配）
    Interact InteractConfig `mapstructure:"interact"`
    // Enable/Sudo 提权命令与提示匹配
    EnableCLI          string `mapstructure:"enable_cli"`
    EnableExceptOutput string `mapstructure:"except_output"`
    // 进入配置模式命令（按序尝试）
    ConfigModeCLIs []string `mapstructure:"config_mode_clis"`
    // 退出配置模式命令（新增）
    ConfigExitCLI string `mapstructure:"config_exit_cli"`
    // 新增：交互时序与节奏参数（旧直出字段，仍保留以兼容老配置）
    CommandIntervalMS         int `mapstructure:"command_interval_ms"`
    CommandTimeoutSec         int `mapstructure:"command_timeout_sec"`
    QuietAfterMS              int `mapstructure:"quiet_after_ms"`
    QuietPollIntervalMS       int `mapstructure:"quiet_poll_interval_ms"`
    EnablePasswordFallbackMS  int `mapstructure:"enable_password_fallback_ms"`
    PromptInducerIntervalMS   int `mapstructure:"prompt_inducer_interval_ms"`
    PromptInducerMaxCount     int `mapstructure:"prompt_inducer_max_count"`
    ExitPauseMS               int `mapstructure:"exit_pause_ms"`
    // 新增：平台层嵌套 timeout（结构与全局 ssh.timeout 一致）
    Timeout PlatformTimeoutConfig `mapstructure:"timeout"`
}
