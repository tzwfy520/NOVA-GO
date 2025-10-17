# 数据格式化服务与接口开发文档

本文档描述在现有采集与 MinIO 对接基础上新增的数据格式化服务与接口的设计、参数规范、并发策略与存储路径约定。该实现以低耦合、复用现有设备登录与命令采集能力为目标，统一通过 `internal/service/format.go` 提供数据格式化处理能力。

## 概述

- 新增服务文件：`internal/service/format.go`
- 新增接口处理器：`api/handler/formatted.go`
- 新增路由：`POST /api/v1/formatted/batch`
- 新增配置段：`data_format.minio_prefix`，用于格式化数据的 MinIO 路径前缀
- 并发策略：沿用 `collector.concurrent` 或并发档位 `collector.concurrency_profile`（档位映射包含 `concurrent` 与 `threads`，其中 `threads` 覆盖 SSH 会话上限）

## 并发策略

- 服务层：`FormatService` 内部以 `cfg.Collector.Concurrent` 作为 Worker 容量，使用信号量控制批内并发执行；同时使用 `cfg.Collector.Threads` 覆盖连接池的会话上限（`ssh.MaxSessions`）。
- 路由层：接口处理器不另行限流，直接进入服务层，由服务层并发策略统一控制。
- 档位：如配置了 `collector.concurrency_profile`（S/M/L/XL），其映射会在启动时下沉到配置：`concurrent` 用于并发度，`threads` 用于会话上限；若未配置档位，则按 `concurrent`/`threads` 数值使用。

## 接口定义

- 路径：`POST /api/v1/formatted/batch`
- 输入参数（JSON）：

```json
{
  "task_id": "CC-20251016-TASK-1",
  "task_name": "custom-batch-check",
  "task_batch": 2,
  "retry_flag": 2,
  "save_dir": "cc_task",
  "timeout": 15,
  "fsm_templates": [
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        { "cli_name": "show version", "fsm_value": "test fsm111\n" },
        { "cli_name": "show version", "fsm_value": "test fsm222\n" }
      ]
    }
  ],
  "devices": [
    {
      "device_ip": "10.200.40.201",
      "device_port": 22,
      "device_name": "test-out-r1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "eccom123",
      "password": "Eccom@12345",
      "cli_list": ["show version", "show runn"]
    }
  ]
}
```

- 输出参数（JSON）：

```json
{
  "code": "SUCCESS",
  "message": "批量格式化处理完成",
  "json_prefix": "/{minio_prefix}/{save_dir}/{task_id}/formatted/",
  "date_time": "20251016_111111",
  "login_failures": [
    {"device_ip":"10.0.0.1","device_name":"ios-1","device_platform":"cisco_ios","error":"auth failed"}
  ],
  "collect_failures": [
    {"device_ip":"10.0.0.1","device_name":"ios-1","device_platform":"cisco_ios","failed_commands":["show runn"]}
  ],
  "format_failures": [
    {"device_ip":"10.0.0.2","device_name":"sw-2","device_platform":"huawei_s","failed_commands":["display version"]}
  ],
  "stats": {
    "total_devices": 10,
    "fully_success_devices": 7,
    "login_failed_devices": 2,
    "parse_failed_devices": 1
  },
  "stored_objects": [
    {"uri":"minio://bucket/{minio_prefix}/cc_task/CC-20251016-TASK-1/formatted/cisco_ios/show_version/formatted_2.json","size":12345,"content_type":"application/json; charset=utf-8"}
  ]
}
```

说明：
- `json_prefix` 为聚合 JSON 存储路径的前缀部分，到设备名的上一层，即 `/{minio_prefix}/{save_dir}/{task_id}/formatted/`。
- `date_time` 使用服务接收任务的时间戳，格式 `YYYYMMDD_HHMMSS`，同一批次所有设备共用该值。
- `stored_objects` 返回部分成功写入的对象信息，便于客户端校验与追踪。

## 逻辑流程

1. 读取接口参数，按设备并发采集信息（复用设备登录与命令采集能力）。
2. 基于设备类型与命令，从请求中的 `fsm_templates` 读取对应 FSM 模板。
3. 结合采集信息与 FSM 模板生成格式化数据（当前采用占位实现：记录模板标识与原始文本，可替换为真实 FSM 引擎）。
4. 组织存储路径并将格式化 JSON 与原始数据分别写入 MinIO。
5. 统计成功/失败信息，聚合响应输出。

## 存储规则与路径

- 配置项：`data_format.minio_prefix`（顶层路径前缀），示例：
  - 开发环境（`configs/dev.yaml`）：`data-formats-dev`
  - 生产环境（`configs/prod.yaml`）：`data-formats`

- 格式化 JSON：
  - 路径示例：`/{minio_prefix}/{save_dir}/{task_id}/formatted/{device_platform}/{cli_name}/formatted_{batch_id}.json`
  - 文件内容：按 `platform+cli` 聚合的数组，每项包含 `device_name` 与 `info_formatted` 字段。

- 原始数据：
  - 路径示例：`/{minio_prefix}/{save_dir}/{task_id}/raw/{batch_id}/{device_name}/formatted/{cli_name}.txt`
  - 文件内容：该设备该命令的原始输出文本。

路径片段说明：
- `minio_prefix`：取自配置 `data_format.minio_prefix`。
- `save_dir`：取自接口入参。
- `task_id`：取自接口入参。
- `batch_id`：取自接口入参 `task_batch`，缺省时默认为 `1`。
- `device_platform`/`cli_name` 采用统一的 `slug` 规则：小写、空格转下划线、去除非法分隔符。

## 失败与统计设计

- 登录失败/超时：记录在 `login_failures`，包括设备标识与错误摘要。
- 采集失败命令：记录在 `collect_failures`，按设备组织失败命令列表。
- 格式化失败命令：记录在 `format_failures`，按设备组织失败命令列表。
- 统计：
  - `total_devices`：请求设备总数。
  - `fully_success_devices`：不含登录失败与解析失败的设备数。
  - `login_failed_devices`：登录失败设备数。
  - `parse_failed_devices`：格式化失败涉及设备的唯一计数。

## 代码结构与耦合控制

- `internal/service/format.go`：
  - 定义 `FormatBatchRequest`、`FormatBatchResponse`、`FormattedItem` 等类型，避免对采集服务类型的依赖。
  - 通过统一交互入口 `InteractBasic` 执行设备登录与命令；交互优先、失败回退非交互的执行逻辑已内联到 `InteractBasic`，并在返回时过滤掉 `enable` 与关闭分页等预命令结果。
  - `FormatMinioWriter` 独立实现 MinIO 写入逻辑（连接检查、重试与桶保证），降低对备份服务的代码依赖。

- `api/handler/formatted.go`：
  - 绑定请求到 `service.FormatBatchRequest`，调用 `FormatService.ExecuteBatch` 输出统一响应。

- `api/router/router.go` 与 `cmd/server/main.go`：
  - 注入并启动 `FormatService`，注册 `POST /api/v1/formatted/batch` 路由。

## 可插拔 FSM 说明

当前 `applyFSM` 为占位实现：当提供 `fsm_templates` 时，返回一个包含 `template` 与 `parsed` 的对象，视为示例处理。实际项目可替换为：
- 将 `fsm_value` 解析为 FSM 状态机或正则/DSL，应用于原始输出生成结构化结果。
- `info_formatted` 字段可承载任意 JSON（对象或数组），以便后续数据消费。

## 注意事项

- MinIO 配置项必须完整（`host`/`port`/`access_key`/`secret_key`/`bucket`），否则写入器会告警并拒绝写入。
- 格式化与原始数据写入采用有限重试；若写入失败不影响整体响应生成，但会记录到日志与 `stored_objects` 不包含该对象。
- 行过滤（分页提示等）未默认应用于原始数据写入，以保留完整输出；如需与采集过滤保持一致可在 `FormatService` 中接入 `collector.output_filter`。

## 版本与扩展

- 版本：v1（本次新增）
- 扩展建议：
  - 增加 `dry_run` 参数用于不写存储仅返回聚合结果。
  - 增加 `per_device_json` 输出，每设备每命令生成独立格式化 JSON（当前按 `platform+cli` 聚合）。
  - 增加 `checksum`/`etag` 计算与返回，便于下游验证。