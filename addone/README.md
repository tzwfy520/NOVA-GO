# SSH采集器插件系统（新设计）

本目录包含项目自带的“采集平台”插件体系。平台采用“厂商 + 设备系统”分类，例如：
- `cisco_ios`、`cisco_nxos`、`cisco_iosxe`、`cisco_iosxr`
- `huawei_s`、`huawei_ce`、`huawei_ne`
- `h3c_s`、`h3c_sr`

## 目录结构

```
addone/
├── interact/                 # 交互插件（会话与命令转换）
│   ├── default.go            # 系统默认交互参数与转换
│   ├── registry.go           # 插件注册中心
│   └── platforms/
│       ├── cisco_ios/
│       │   └── plugin.go     # Cisco IOS 交互插件（示例：enable）
│       ├── huawei_s/
│       │   └── plugin.go     # 华为 S 交互插件
│       └── huawei_ce/
│           └── plugin.go     # 华为 CE 交互插件
└── collect/                  # 采集插件（存储与解析）
    ├── default.go            # 默认存储配置与直通解析
    ├── registry.go           # 插件注册中心
    └── platforms/
        ├── cisco_ios/
        │   └── show_run.go       # show run / show version 解析存根
        ├── huawei_s/
        │   └── display_current_config.go
        └── huawei_ce/
            └── display_current_config.go
```

## 交互插件

- default 插件：定义系统默认的 `Timeout/Retries/Threads/Concurrent` 等参数，所有采集操作遵守默认值。
- 采集平台插件：覆盖默认参数并在命令执行前进行平台特定的转换，例如 `cisco_ios` 插件按需在命令前插入 `enable`。
- 当平台插件参数与 default 不一致时，以平台插件为准。

接口摘录：
```go
type InteractDefaults struct {
    Timeout int; Retries int; Threads int; Concurrent int
}
type InteractPlugin interface {
    Name() string
    Defaults() InteractDefaults
    TransformCommands(in CommandTransformInput) CommandTransformOutput
}
```

## 采集插件

- default 插件：定义默认的采集结果存储位置
  1) 原始数据对象存储（Minio）：`Host/Port/AccessKey/SecretKey/Bucket`
  2) 格式化数据目标数据库：`Type/Host/Port/Username/Password/Database/Tables`
- 采集平台插件：按“平台 + 命令”组织，提供内置解析逻辑。
- 采集命令文件名以命令空格替换为下划线组织，例如：`display current-configuration` → `display_current_config.go`。

接口摘录：
```go
type CollectPlugin interface {
    Name() string
    StorageDefaults() StorageDefaults
    Parse(ctx ParseContext, raw string) (ParseResult, error)
}
```

## 使用建议

- 通过 `metadata["platform"]` 指定平台标识（如：`huawei_s`），在业务层选择交互/采集插件。
- 交互插件的 `TransformCommands` 可用于插入前置命令或特权模式切换。
- 采集插件的 `Parse` 用于将原始输出解析为结构化数据，并结合 `StorageDefaults` 决定落库策略。

## 扩展开发

1. 在 `interact/platforms/<platform>/` 创建新插件，注册到中心。
2. 在 `collect/platforms/<platform>/` 按命令创建解析文件，实现 `Parse` 逻辑。
3. 在业务侧（例如 `internal/service/collector.go`）按需接入交互转换与结果解析逻辑。