package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const defaultPlatformKey = "default"

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
	Host           string        `mapstructure:"host"`
	Port           int           `mapstructure:"port"`
	Mode           string        `mapstructure:"mode"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	SimulateEnable bool          `mapstructure:"simulate_enable"`
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
	// Interact 全局交互节奏（来自 ssh.timeout.interact_timeout；平台可覆盖）
	Interact InteractTimingConfig `mapstructure:"-"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string                     `mapstructure:"level"`
	Format     string                     `mapstructure:"format"`
	Output     string                     `mapstructure:"output"`
	FilePath   string                     `mapstructure:"file_path"`
	MaxSize    int                        `mapstructure:"max_size"`
	MaxBackups int                        `mapstructure:"max_backups"`
	MaxAge     int                        `mapstructure:"max_age"`
	Compress   bool                       `mapstructure:"compress"`
	Modules    map[string]ModuleLogConfig `mapstructure:"modules"`
	TaskLogs   TaskLogsConfig             `mapstructure:"task_logs"`
}

type ModuleLogConfig struct {
	Level string `mapstructure:"level"`
}

type TaskLogsConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	RetentionDays  int           `mapstructure:"retention_days"`
	MaxFiles       int           `mapstructure:"max_files"`
	MaxTotalSizeMB int64         `mapstructure:"max_total_size_mb"`
	ScanInterval   time.Duration `mapstructure:"scan_interval"`
	Directories    []string      `mapstructure:"directories"`
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
		to := viper.GetInt("ssh.timeout.timeout_all") // 改为GetInt
		if to > 0 {
			config.SSH.Timeout = time.Duration(to) * time.Second // 转换为time.Duration
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
	// 读取全局交互节奏：ssh.timeout.interact_timeout.*
	// 说明：该块不直接映射到结构体（避免与旧键冲突），这里手动读取并写入 config.SSH.Interact
	{
		var it InteractTimingConfig
		it.CommandIntervalMS = viper.GetInt("ssh.timeout.interact_timeout.command_interval_ms")
		it.CommandTimeoutSec = viper.GetInt("ssh.timeout.interact_timeout.command_timeout_sec")
		it.QuietAfterMS = viper.GetInt("ssh.timeout.interact_timeout.quiet_after_ms")
		it.QuietPollIntervalMS = viper.GetInt("ssh.timeout.interact_timeout.quiet_poll_interval_ms")
		it.EnablePasswordFallbackMS = viper.GetInt("ssh.timeout.interact_timeout.enable_password_fallback_ms")
		it.PromptInducerIntervalMS = viper.GetInt("ssh.timeout.interact_timeout.prompt_inducer_interval_ms")
		it.PromptInducerMaxCount = viper.GetInt("ssh.timeout.interact_timeout.prompt_inducer_max_count")
		it.ExitPauseMS = viper.GetInt("ssh.timeout.interact_timeout.exit_pause_ms")
		config.SSH.Interact = it
	}

	// 环境变量替换
	config = replaceEnvVars(config)

	// 读取 auto-ssh.yaml 的设备平台默认项并合并：
	// - auto-ssh.yaml 作为内置默认库
	// - 主配置文件中显式设置的字段覆盖 auto-ssh.yaml（支持显式设置 false / 空数组）
	autoPath := filepath.Join("configs", "auto-ssh.yaml")
	if dd, err := loadAutoSSHDeviceDefaults(autoPath); err == nil && len(dd) > 0 {
		main := config.Collector.DeviceDefaults
		presence := buildDeviceDefaultsPresence(main)
		merged := make(map[string]PlatformDefaultsConfig, len(dd)+len(main))
		for p, v := range dd {
			merged[p] = v
		}
		for p, mv := range main {
			if av, ok := merged[p]; ok {
				merged[p] = mergePlatformDefaultsByPresence(av, mv, presence[p])
			} else {
				merged[p] = mv
			}
		}
		config.Collector.DeviceDefaults = merged
	}

	// 应用并发档位配置（若设置了 concurrency_profile 则覆盖 concurrent 数值）
	applyConcurrencyProfile(&config)

	globalConfig = &config
	return &config, nil
}

type deviceDefaultsPresence struct {
	PromptSuffixes           bool
	DisablePagingCmds        bool
	AutoInteractions         bool
	ErrorHints               bool
	SkipDelayedEcho          bool
	EnableRequired           bool
	LongOutputCommands       bool
	EnableCLI                bool
	EnableExceptOutput       bool
	ConfigModeCLIs           bool
	ConfigExitCLI            bool
	CommandIntervalMS        bool
	CommandTimeoutSec        bool
	QuietAfterMS             bool
	QuietPollIntervalMS      bool
	EnablePasswordFallbackMS bool
	PromptInducerIntervalMS  bool
	PromptInducerMaxCount    bool
	ExitPauseMS              bool

	OutputFilterPrefixes        bool
	OutputFilterContains        bool
	OutputFilterCaseInsensitive bool
	OutputFilterTrimSpace       bool

	InteractAutoInteractions bool
	InteractErrorHints       bool
	InteractCaseInsensitive  bool
	InteractTrimSpace        bool

	TimeoutAll     bool
	DialTimeoutSec bool
	AuthTimeoutSec bool

	TimeoutInteractCommandIntervalMS        bool
	TimeoutInteractCommandTimeoutSec        bool
	TimeoutInteractQuietAfterMS             bool
	TimeoutInteractQuietPollIntervalMS      bool
	TimeoutInteractEnablePasswordFallbackMS bool
	TimeoutInteractPromptInducerIntervalMS  bool
	TimeoutInteractPromptInducerMaxCount    bool
	TimeoutInteractExitPauseMS              bool
}

func buildDeviceDefaultsPresence(main map[string]PlatformDefaultsConfig) map[string]deviceDefaultsPresence {
	out := make(map[string]deviceDefaultsPresence, len(main))
	for p := range main {
		key := "collector.device_defaults." + p + "."
		out[p] = deviceDefaultsPresence{
			PromptSuffixes:           viper.IsSet(key + "prompt_suffixes"),
			DisablePagingCmds:        viper.IsSet(key + "disable_paging_cmds"),
			AutoInteractions:         viper.IsSet(key + "auto_interactions"),
			ErrorHints:               viper.IsSet(key + "error_hints"),
			SkipDelayedEcho:          viper.IsSet(key + "skip_delayed_echo"),
			EnableRequired:           viper.IsSet(key + "enable_required"),
			LongOutputCommands:       viper.IsSet(key + "long_output_commands"),
			EnableCLI:                viper.IsSet(key + "enable_cli"),
			EnableExceptOutput:       viper.IsSet(key + "enable_except_output"),
			ConfigModeCLIs:           viper.IsSet(key + "config_mode_clis"),
			ConfigExitCLI:            viper.IsSet(key + "config_exit_cli"),
			CommandIntervalMS:        viper.IsSet(key + "command_interval_ms"),
			CommandTimeoutSec:        viper.IsSet(key + "command_timeout_sec"),
			QuietAfterMS:             viper.IsSet(key + "quiet_after_ms"),
			QuietPollIntervalMS:      viper.IsSet(key + "quiet_poll_interval_ms"),
			EnablePasswordFallbackMS: viper.IsSet(key + "enable_password_fallback_ms"),
			PromptInducerIntervalMS:  viper.IsSet(key + "prompt_inducer_interval_ms"),
			PromptInducerMaxCount:    viper.IsSet(key + "prompt_inducer_max_count"),
			ExitPauseMS:              viper.IsSet(key + "exit_pause_ms"),

			OutputFilterPrefixes:        viper.IsSet(key + "output_filter.prefixes"),
			OutputFilterContains:        viper.IsSet(key + "output_filter.contains"),
			OutputFilterCaseInsensitive: viper.IsSet(key + "output_filter.case_insensitive"),
			OutputFilterTrimSpace:       viper.IsSet(key + "output_filter.trim_space"),

			InteractAutoInteractions: viper.IsSet(key + "interact.auto_interactions"),
			InteractErrorHints:       viper.IsSet(key + "interact.error_hints"),
			InteractCaseInsensitive:  viper.IsSet(key + "interact.case_insensitive"),
			InteractTrimSpace:        viper.IsSet(key + "interact.trim_space"),

			TimeoutAll:     viper.IsSet(key + "timeout.timeout_all"),
			DialTimeoutSec: viper.IsSet(key + "timeout.dial_timeout"),
			AuthTimeoutSec: viper.IsSet(key + "timeout.auth_timeout"),

			TimeoutInteractCommandIntervalMS:        viper.IsSet(key + "timeout.interact_timeout.command_interval_ms"),
			TimeoutInteractCommandTimeoutSec:        viper.IsSet(key + "timeout.interact_timeout.command_timeout_sec"),
			TimeoutInteractQuietAfterMS:             viper.IsSet(key + "timeout.interact_timeout.quiet_after_ms"),
			TimeoutInteractQuietPollIntervalMS:      viper.IsSet(key + "timeout.interact_timeout.quiet_poll_interval_ms"),
			TimeoutInteractEnablePasswordFallbackMS: viper.IsSet(key + "timeout.interact_timeout.enable_password_fallback_ms"),
			TimeoutInteractPromptInducerIntervalMS:  viper.IsSet(key + "timeout.interact_timeout.prompt_inducer_interval_ms"),
			TimeoutInteractPromptInducerMaxCount:    viper.IsSet(key + "timeout.interact_timeout.prompt_inducer_max_count"),
			TimeoutInteractExitPauseMS:              viper.IsSet(key + "timeout.interact_timeout.exit_pause_ms"),
		}
	}
	return out
}

func mergePlatformDefaultsByPresence(base PlatformDefaultsConfig, override PlatformDefaultsConfig, pres deviceDefaultsPresence) PlatformDefaultsConfig {
	if pres.PromptSuffixes {
		base.PromptSuffixes = append([]string{}, override.PromptSuffixes...)
	}
	if pres.DisablePagingCmds {
		base.DisablePagingCmds = append([]string{}, override.DisablePagingCmds...)
	}
	if pres.AutoInteractions {
		base.AutoInteractions = append([]AutoInteractionConfig{}, override.AutoInteractions...)
	}
	if pres.ErrorHints {
		base.ErrorHints = append([]string{}, override.ErrorHints...)
	}
	if pres.SkipDelayedEcho {
		base.SkipDelayedEcho = override.SkipDelayedEcho
	}
	if pres.EnableRequired {
		base.EnableRequired = override.EnableRequired
	}
	if pres.LongOutputCommands {
		base.LongOutputCommands = append([]string{}, override.LongOutputCommands...)
	}
	if pres.EnableCLI {
		base.EnableCLI = override.EnableCLI
	}
	if pres.EnableExceptOutput {
		base.EnableExceptOutput = override.EnableExceptOutput
	}
	if pres.ConfigModeCLIs {
		base.ConfigModeCLIs = append([]string{}, override.ConfigModeCLIs...)
	}
	if pres.ConfigExitCLI {
		base.ConfigExitCLI = override.ConfigExitCLI
	}

	if pres.CommandIntervalMS {
		base.CommandIntervalMS = override.CommandIntervalMS
	}
	if pres.CommandTimeoutSec {
		base.CommandTimeoutSec = override.CommandTimeoutSec
	}
	if pres.QuietAfterMS {
		base.QuietAfterMS = override.QuietAfterMS
	}
	if pres.QuietPollIntervalMS {
		base.QuietPollIntervalMS = override.QuietPollIntervalMS
	}
	if pres.EnablePasswordFallbackMS {
		base.EnablePasswordFallbackMS = override.EnablePasswordFallbackMS
	}
	if pres.PromptInducerIntervalMS {
		base.PromptInducerIntervalMS = override.PromptInducerIntervalMS
	}
	if pres.PromptInducerMaxCount {
		base.PromptInducerMaxCount = override.PromptInducerMaxCount
	}
	if pres.ExitPauseMS {
		base.ExitPauseMS = override.ExitPauseMS
	}

	if pres.OutputFilterPrefixes {
		base.OutputFilter.Prefixes = append([]string{}, override.OutputFilter.Prefixes...)
	}
	if pres.OutputFilterContains {
		base.OutputFilter.Contains = append([]string{}, override.OutputFilter.Contains...)
	}
	if pres.OutputFilterCaseInsensitive {
		base.OutputFilter.CaseInsensitive = override.OutputFilter.CaseInsensitive
	}
	if pres.OutputFilterTrimSpace {
		base.OutputFilter.TrimSpace = override.OutputFilter.TrimSpace
	}

	if pres.InteractAutoInteractions {
		base.Interact.AutoInteractions = append([]AutoInteractionConfig{}, override.Interact.AutoInteractions...)
	}
	if pres.InteractErrorHints {
		base.Interact.ErrorHints = append([]string{}, override.Interact.ErrorHints...)
	}
	if pres.InteractCaseInsensitive {
		base.Interact.CaseInsensitive = override.Interact.CaseInsensitive
	}
	if pres.InteractTrimSpace {
		base.Interact.TrimSpace = override.Interact.TrimSpace
	}

	if pres.TimeoutAll {
		base.Timeout.TimeoutAll = override.Timeout.TimeoutAll
	}
	if pres.DialTimeoutSec {
		base.Timeout.DialTimeoutSec = override.Timeout.DialTimeoutSec
	}
	if pres.AuthTimeoutSec {
		base.Timeout.AuthTimeoutSec = override.Timeout.AuthTimeoutSec
	}
	if pres.TimeoutInteractCommandIntervalMS {
		base.Timeout.Interact.CommandIntervalMS = override.Timeout.Interact.CommandIntervalMS
	}
	if pres.TimeoutInteractCommandTimeoutSec {
		base.Timeout.Interact.CommandTimeoutSec = override.Timeout.Interact.CommandTimeoutSec
	}
	if pres.TimeoutInteractQuietAfterMS {
		base.Timeout.Interact.QuietAfterMS = override.Timeout.Interact.QuietAfterMS
	}
	if pres.TimeoutInteractQuietPollIntervalMS {
		base.Timeout.Interact.QuietPollIntervalMS = override.Timeout.Interact.QuietPollIntervalMS
	}
	if pres.TimeoutInteractEnablePasswordFallbackMS {
		base.Timeout.Interact.EnablePasswordFallbackMS = override.Timeout.Interact.EnablePasswordFallbackMS
	}
	if pres.TimeoutInteractPromptInducerIntervalMS {
		base.Timeout.Interact.PromptInducerIntervalMS = override.Timeout.Interact.PromptInducerIntervalMS
	}
	if pres.TimeoutInteractPromptInducerMaxCount {
		base.Timeout.Interact.PromptInducerMaxCount = override.Timeout.Interact.PromptInducerMaxCount
	}
	if pres.TimeoutInteractExitPauseMS {
		base.Timeout.Interact.ExitPauseMS = override.Timeout.Interact.ExitPauseMS
	}

	return base
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
	viper.SetDefault("ssh.timeout.timeout_all", 60) // 改为int类型，单位秒
	// 拨号与握手阶段拆分默认（合并为 ConnectTimeout 使用）
	viper.SetDefault("ssh.timeout.dial_timeout", 2)
	viper.SetDefault("ssh.timeout.auth_timeout", 5)
	// 全局交互节奏默认（平台可覆盖）
	viper.SetDefault("ssh.timeout.interact_timeout.command_interval_ms", 120)
	viper.SetDefault("ssh.timeout.interact_timeout.command_timeout_sec", 30)
	viper.SetDefault("ssh.timeout.interact_timeout.quiet_after_ms", 800)
	viper.SetDefault("ssh.timeout.interact_timeout.quiet_poll_interval_ms", 250)
	viper.SetDefault("ssh.timeout.interact_timeout.prompt_inducer_interval_ms", 1000)
	viper.SetDefault("ssh.timeout.interact_timeout.prompt_inducer_max_count", 12)
	viper.SetDefault("ssh.timeout.interact_timeout.exit_pause_ms", 150)
	viper.SetDefault("ssh.timeout.interact_timeout.enable_password_fallback_ms", 1500)

	// 新增：连接池清理周期默认 30s（可通过 ssh.cleanup_interval 覆盖）
	viper.SetDefault("ssh.cleanup_interval", 30*time.Second)

	// 新增：模拟服务开关默认关闭
	viper.SetDefault("server.simulate_enable", false)

	// 新增：日志默认级别为 info（可通过 log.level 覆盖为 debug/warn/error 等）
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.task_logs.enabled", true)
	viper.SetDefault("log.task_logs.retention_days", 7)
	viper.SetDefault("log.task_logs.max_files", 5000)
	viper.SetDefault("log.task_logs.max_total_size_mb", 2048)
	viper.SetDefault("log.task_logs.scan_interval", 30*time.Minute)
	viper.SetDefault("log.task_logs.directories", []string{"./logs/collection", "./logs/backup"})
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

	// 应用档位配置（若存在映射）
	if profConf, ok := mapping[strings.ToUpper(p)]; ok {
		if profConf.Concurrent > 0 {
			cfg.Collector.Concurrent = profConf.Concurrent
		}
		if profConf.Threads > 0 {
			cfg.Collector.Threads = profConf.Threads
		}
	}
}

// 从 auto-ssh.yaml 加载设备平台默认项（collector.device_defaults 或顶层 device_defaults）
func loadAutoSSHDeviceDefaults(path string) (map[string]PlatformDefaultsConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	type collectorWrapper struct {
		DeviceDefaults map[string]PlatformDefaultsConfig `mapstructure:"device_defaults"`
	}
	var root struct {
		Collector      collectorWrapper                  `mapstructure:"collector"`
		DeviceDefaults map[string]PlatformDefaultsConfig `mapstructure:"device_defaults"`
	}
	if err := v.Unmarshal(&root); err != nil {
		return nil, err
	}
	if len(root.Collector.DeviceDefaults) > 0 {
		return root.Collector.DeviceDefaults, nil
	}
	if len(root.DeviceDefaults) > 0 {
		return root.DeviceDefaults, nil
	}
	return nil, fmt.Errorf("device_defaults not found in %s", path)
}

// GetServerAddr 获取服务器地址（Host+Port）
func (c *Config) GetServerAddr() string {
	if strings.TrimSpace(c.Server.Host) == "" {
		return fmt.Sprintf(":%d", c.Server.Port)
	}
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func (c *Config) ResolvePlatformKey(platform string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	if p == "" {
		return defaultPlatformKey
	}
	if c == nil || c.Collector.DeviceDefaults == nil {
		return p
	}
	if _, ok := c.Collector.DeviceDefaults[p]; ok {
		return p
	}
	if strings.HasPrefix(p, "cisco") {
		if _, ok := c.Collector.DeviceDefaults["cisco_ios"]; ok {
			return "cisco_ios"
		}
	}
	if strings.HasPrefix(p, "huawei") {
		if _, ok := c.Collector.DeviceDefaults["huawei"]; ok {
			return "huawei"
		}
	}
	if strings.HasPrefix(p, "h3c") {
		if _, ok := c.Collector.DeviceDefaults["h3c"]; ok {
			return "h3c"
		}
	}
	if strings.HasPrefix(p, "linux") {
		if _, ok := c.Collector.DeviceDefaults["linux"]; ok {
			return "linux"
		}
	}
	bestKey := ""
	bestLen := 0
	for k := range c.Collector.DeviceDefaults {
		kk := strings.ToLower(strings.TrimSpace(k))
		if kk == "" || kk == defaultPlatformKey {
			continue
		}
		if strings.HasPrefix(p, kk) && len(kk) > bestLen {
			bestKey = kk
			bestLen = len(kk)
		}
	}
	if bestKey != "" {
		return bestKey
	}
	if _, ok := c.Collector.DeviceDefaults[defaultPlatformKey]; ok {
		return defaultPlatformKey
	}
	return p
}

func (c *Config) GetDeviceDefaults(platform string) (PlatformDefaultsConfig, string, bool) {
	if c == nil || c.Collector.DeviceDefaults == nil {
		return PlatformDefaultsConfig{}, "", false
	}
	key := c.ResolvePlatformKey(platform)
	if v, ok := c.Collector.DeviceDefaults[key]; ok {
		return v, key, true
	}
	if key != "default" {
		if v, ok := c.Collector.DeviceDefaults["default"]; ok {
			return v, "default", true
		}
	}
	return PlatformDefaultsConfig{}, key, false
}

// GetTimeoutAll 获取某个平台的总超时（若平台未定义则返回全局默认）
func (c *Config) GetTimeoutAll(platform string) int {
	if c != nil {
		if def, _, ok := c.GetDeviceDefaults(platform); ok {
			if def.Timeout.TimeoutAll > 0 {
				return def.Timeout.TimeoutAll
			}
		}
	}
	// 若平台未定义或未设置，返回全局默认
	if c.SSH.Timeout > 0 {
		return int(c.SSH.Timeout / time.Second)
	}
	if c.SSH.ConnectTimeout > 0 {
		return int(c.SSH.ConnectTimeout / time.Second)
	}
	// 若均未设置，返回默认 60s
	return 60
}

// OutputFilterConfig 输出过滤配置
type OutputFilterConfig struct {
	Prefixes        []string `mapstructure:"prefixes"`
	Contains        []string `mapstructure:"contains"`
	CaseInsensitive bool     `mapstructure:"case_insensitive"`
	TrimSpace       bool     `mapstructure:"trim_space"`
}

// InteractConfig 交互配置（提示符、自动交互与错误提示）
type InteractConfig struct {
	AutoInteractions []AutoInteractionConfig `mapstructure:"auto_interactions"`
	ErrorHints       []string                `mapstructure:"error_hints"`
	CaseInsensitive  bool                    `mapstructure:"case_insensitive"`
	TrimSpace        bool                    `mapstructure:"trim_space"`
}

// AutoInteractionConfig 自动交互配置（提示输出匹配与自动下发）
type AutoInteractionConfig struct {
	ExpectOutput string `mapstructure:"except_output"`
	AutoSend     string `mapstructure:"command_auto_send"`
}

// InteractTimingConfig 交互时序相关配置（毫秒与秒，以平台覆盖全局）
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

// PlatformTimeoutConfig 平台超时配置（与全局 SSH 超时合并使用）
type PlatformTimeoutConfig struct {
	TimeoutAll     int                  `mapstructure:"timeout_all"` // 改为int类型（秒）
	DialTimeoutSec int                  `mapstructure:"dial_timeout"`
	AuthTimeoutSec int                  `mapstructure:"auth_timeout"`
	Interact       InteractTimingConfig `mapstructure:"interact_timeout"`
}

// PlatformDefaultsConfig 平台默认交互/适配参数
type PlatformDefaultsConfig struct {
	PromptSuffixes    []string                `mapstructure:"prompt_suffixes"`
	DisablePagingCmds []string                `mapstructure:"disable_paging_cmds"`
	AutoInteractions  []AutoInteractionConfig `mapstructure:"auto_interactions"`
	ErrorHints        []string                `mapstructure:"error_hints"`
	SkipDelayedEcho   bool                    `mapstructure:"skip_delayed_echo"`
	EnableRequired    bool                    `mapstructure:"enable_required"`

	LongOutputCommands []string `mapstructure:"long_output_commands"`

	OutputFilter OutputFilterConfig `mapstructure:"output_filter"`

	Interact InteractConfig `mapstructure:"interact"`

	EnableCLI          string `mapstructure:"enable_cli"`
	EnableExceptOutput string `mapstructure:"enable_except_output"`

	ConfigModeCLIs []string `mapstructure:"config_mode_clis"`

	ConfigExitCLI string `mapstructure:"config_exit_cli"`

	CommandIntervalMS        int `mapstructure:"command_interval_ms"`
	CommandTimeoutSec        int `mapstructure:"command_timeout_sec"`
	QuietAfterMS             int `mapstructure:"quiet_after_ms"`
	QuietPollIntervalMS      int `mapstructure:"quiet_poll_interval_ms"`
	EnablePasswordFallbackMS int `mapstructure:"enable_password_fallback_ms"`
	PromptInducerIntervalMS  int `mapstructure:"prompt_inducer_interval_ms"`
	PromptInducerMaxCount    int `mapstructure:"prompt_inducer_max_count"`
	ExitPauseMS              int `mapstructure:"exit_pause_ms"`

	Timeout PlatformTimeoutConfig `mapstructure:"timeout"`
}
