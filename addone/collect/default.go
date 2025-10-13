package collect

import (
    "github.com/sshcollectorpro/sshcollectorpro/internal/config"
)

// MinioConfig 原始数据对象存储配置
type MinioConfig struct {
	Host      string
	Port      int
	AccessKey string
	SecretKey string
	Bucket    string
	Secure    bool
}

// DBConfig 格式化数据存储配置
type DBConfig struct {
	Type     string // e.g. sqlite, mysql, postgres
	Host     string
	Port     int
	Username string
	Password string
	Database string
	// 由系统支持的表清单名称（如：device_info, interfaces, vlans 等）
	Tables []string
}

// StorageDefaults 存储默认配置
type StorageDefaults struct {
	RawStore MinioConfig
	DBStore  DBConfig
}

// ParseContext 解析上下文
type ParseContext struct {
	Platform string
	Command  string
	// 以下信息用于落库与拼接
	TaskID   string
	Status   string        // 任务状态（success/failed）
	RawPaths RawStorePaths // 原始数据映射（命令->对象路径）
}

// ParseOutput 解析输出（用于格式化入库）
type ParseOutput struct {
	Platform string
	Command  string
	Raw      string
	Rows     []FormattedRow
}

// CollectPlugin 采集插件接口
type CollectPlugin interface {
    Name() string
    StorageDefaults() StorageDefaults
    // SystemCommands 返回该平台系统内置采集命令（用于 collect_origin=system）
    SystemCommands() []string
    // Parse 将原始命令输出解析为结构化数据
    Parse(ctx ParseContext, raw string) (ParseOutput, error)
}

// DefaultPlugin 系统默认采集插件
type DefaultPlugin struct{}

func (p *DefaultPlugin) Name() string { return "default" }

func (p *DefaultPlugin) StorageDefaults() StorageDefaults {
    // 默认值
    raw := MinioConfig{
        Host:      "127.0.0.1",
        Port:      9000,
        AccessKey: "minioadmin",
        SecretKey: "minioadmin",
        Bucket:    "sshcollector-raw",
        Secure:    false,
    }
    db := DBConfig{
        Type:     "postgres",
        Host:     "127.0.0.1",
        Port:     5432,
        Username: "postgres",
        Password: "postgres",
        Database: "sshcollector",
        Tables: []string{
            "device_info",
            "version_info",
        },
    }

    // 允许通过配置文件 YAML 在运行时覆盖（不修改 Tables）
    if cfg := config.Get(); cfg != nil {
        // MinIO 覆盖
        if m := cfg.Storage.Minio; m.Host != "" {
            raw.Host = m.Host
        }
        if m := cfg.Storage.Minio; m.Port != 0 {
            raw.Port = m.Port
        }
        if m := cfg.Storage.Minio; m.AccessKey != "" {
            raw.AccessKey = m.AccessKey
        }
        if m := cfg.Storage.Minio; m.SecretKey != "" {
            raw.SecretKey = m.SecretKey
        }
        if m := cfg.Storage.Minio; m.Bucket != "" {
            raw.Bucket = m.Bucket
        }
        // 注意：secure 默认为 false，仅当 YAML 明确设置时覆盖
        if m := cfg.Storage.Minio; m.Secure {
            raw.Secure = true
        }

        // Postgres 覆盖（不改 Type 与 Tables）
        if pg := cfg.Storage.Postgres; pg.Host != "" {
            db.Host = pg.Host
        }
        if pg := cfg.Storage.Postgres; pg.Port != 0 {
            db.Port = pg.Port
        }
        if pg := cfg.Storage.Postgres; pg.Username != "" {
            db.Username = pg.Username
        }
        if pg := cfg.Storage.Postgres; pg.Password != "" {
            db.Password = pg.Password
        }
        if pg := cfg.Storage.Postgres; pg.Database != "" {
            db.Database = pg.Database
        }
    }

    return StorageDefaults{RawStore: raw, DBStore: db}
}

// SystemCommands 默认平台不提供内置命令
func (p *DefaultPlugin) SystemCommands() []string { return []string{} }

func (p *DefaultPlugin) Parse(ctx ParseContext, raw string) (ParseOutput, error) {
	// 默认不解析，直接返回原始数据包裹
	return ParseOutput{
		Platform: ctx.Platform,
		Command:  ctx.Command,
		Raw:      raw,
		Rows:     nil,
	}, nil
}

// 取消环境变量覆盖逻辑，改用 YAML 配置覆盖
