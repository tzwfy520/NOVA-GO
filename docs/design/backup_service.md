# 备份服务设计文档

## 背景与目标
- 为网络设备提供“配置备份”能力，按批次执行指定 CLI 并将采集结果直接存储到本地或 MinIO。
- 与现有采集与 SSH 交互模块尽量解耦，复用能力但保持最小改动。
- 作为独立服务位于 `internal/service/backup.go`，对外暴露批量备份接口 `POST /api/v1/backup/batch`。

## 设计原则
- 解耦：备份服务不直接依赖 `internal/service/collector.go` 的任务编排；通过“命令执行适配器”复用 `pkg/ssh/client.go` 的交互能力。
- 最小改动：新增服务与路由、请求模型与存储写入模块，不改动既有 SSH 客户端逻辑；配置仅新增备份相关键。
- 幂等与可追踪：相同 `task_id + task_batch + device_ip + command` 生成稳定的对象键；输出包含对象 URI 与校验信息。
- 安全：不在存储与日志中泄露密码、启用回退提权时避免密码出现在原始输出。

## 架构与模块
**组件与职责**
- `BackupService (internal/service/backup.go)`：
  - 校验与解析请求、并发调度设备备份、汇总结果并产出响应。
  - 依赖 `CommandExecutor` 执行命令、依赖 `StorageWriter` 写入存储。
- `CommandExecutor (internal/service/exec_adapter.go)`：
  - 接口：`Execute(ctx, req, commands) ([]*ssh.CommandResult, error)`。
  - 默认实现复用 `pkg/ssh/client.go`（交互优先、失败时非交互回退），不修改原逻辑。
- `StorageWriter (internal/service/storage_writer.go)`：
  - 接口：`Write(ctx, meta, content) (StoredObject, error)`，支持 `local` 与 `minio` 两种实现。
  - 负责命名、分层目录/前缀、校验和计算、可选压缩（如 `.txt`/`.json`）。
- `DTOs`：请求/响应/设备/结果/存储对象模型，放置于 `internal/service/backup_types.go`。

**处理流程**
1. Handler 接收 `BackupBatchRequest`（批量）。
2. `BackupService` 构建每设备的执行上下文（合并请求超时与平台默认配置）。
3. 按设备并发（受 `collector.concurrent` 或备份专用并发配置控制），对设备的 `cli_list` 顺序执行：
   - 通过 `CommandExecutor` 执行命令（交互式优先，失败时回退）。
   - 将原始输出写入 `StorageWriter`，返回对象 URI/大小/校验和。
4. 汇总每设备的命令结果与存储对象，产出批量响应。
5. 记录任务与审计日志（沿用现有日志组件）。

## 配置设计（configs/*.yaml）
新增并精简 `backup` 节点（不影响现有配置），示例：
```yaml
backup:
  # 后端：local|minio
  backend: "local"
  # 存储前缀：与接口中的 save_dir 拼接形成最终路径
  prefix: "backups"  # 最终路径：<prefix>/<save_dir>/<device_name>/<date>/<task_id>/<command_slug>.txt
  # 本地存储根目录（可选，默认当前目录）
  local:
    base_dir: "./"      # 最终路径示例：./backups/system_auto/...
    mkdir_if_missing: true
    compress: false
  # 说明：若 backend=minio，连接参数与凭据统一复用 storage.minio 节点；backup 仅保留 prefix
```

说明：备份节点不包含 MinIO 的 `host/port/access_key/secret_key/secure/bucket` 等参数，全部从全局 `storage.minio` 读取；行过滤逻辑也不在备份节点配置，统一复用全局 `collector.output_filter`。

## 接口设计
**Endpoint**
- `POST /api/v1/backup/batch`

**请求模型（JSON）**
```json
{
  "task_id": "T-2001",
  "task_name": "custom-batch-backup",
  "task_batch": 1,
  "save_dir": "system_auto",
  "retry_flag": 1,
  "timeout": 30,
  "devices": [
    {
      "device_ip": "10.0.0.2",
      "device_port": 22,
      "device_name": "ios-edge-1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "ops",
      "password": "xxxx",
      "enable_password": "xxxx",
      "cli_list": ["show version", "show interfaces"]
    }
  ]
}
```

字段说明：
- `save_dir`：用于最终存储路径的拼接，形成 `<prefix>/<save_dir>/...` 的目录层级。
- `task_batch`：同一任务的分批序号，用于对象键命名与审计。
- `cli_list`：按设备定义（与采集接口的顶层 `cli_list` 有区别）。

**响应模型（JSON）**
```json
{
  "code": "SUCCESS",
  "message": "批量备份执行完成",
  "data": [
    {
      "device_ip": "10.0.0.2",
      "port": 22,
      "device_name": "ios-edge-1",
      "device_platform": "cisco_ios",
      "task_id": "T-2001",
      "task_batch": 1,
      "success": true,
      "results": [
        {
          "command": "show version",
          "raw_output": "...",
          "raw_output_lines": ["..."],
          "stored_objects": [
            {
              "uri": "file:///backups/system_auto/ios-edge-1/20251016/T-2001/show-version.txt",
              "size": 12345,
              "checksum": "sha256:...",
              "content_type": "text/plain"
            }
          ],
          "exit_code": 0,
          "duration_ms": 120
        }
      ],
      "error": "",
      "duration_ms": 16000,
      "timestamp": "2025-10-16T11:16:25.251Z"
    }
  ],
  "total": 1
}
```

**错误模型**
- 设备级错误聚合在 `device.error`，命令级错误在 `results[i].error`；服务级错误使用 `code=ERROR` 与详细 `message`。
- 常见错误码：`INVALID_PARAMS`、`EMPTY_DEVICES`、`COMMAND_TIMEOUT`、`STORAGE_WRITE_FAILED`、`UNSUPPORTED_BACKEND`。

## 存储设计
**本地存储**
- 根目录：`backup.local.base_dir`（默认 `./`）。
- 目录层级：`<prefix>/<save_dir>/<device_name>/<date>/<task_id>/`（`device_name` 缺失时回退到 `device_ip`）。
- 文件命名：`{command_slug}.txt` 或 `.json`（可选 gzip 压缩）。
- 行过滤：在写入前统一应用全局 `collector.output_filter`（prefixes/contains/case/trim）。
- 元数据：同目录内写入 `manifest.json`，包含设备信息、命令列表、对象键、校验和与时间戳。

**MinIO 存储**
- Bucket：统一使用全局 `storage.minio.bucket`。
- 对象键：`<prefix>/<save_dir>/<device_name>/<date>/<task_id>/<command_slug>.txt`。
- 对象写入：`Content-Type` 依据原始输出类型（默认 `text/plain; charset=utf-8`）。
- 校验：计算 `sha256` 并写入对象元数据与响应。

## 与现有模块的集成与解耦
- 复用 `pkg/ssh/client.go` 的交互与回退能力，通过 `CommandExecutor` 适配器调用，不修改其代码。
- 不依赖 `internal/service/collector.go` 的任务数据库与执行编排；备份服务仅做轻量的任务审计（可复用日志与可选任务记录模型）。
- 平台默认配置（提示符/提权/错误提示）沿用 `configs/config.yaml` 的 `collector.device_defaults`；备份服务读取并按设备平台应用。
- 输出过滤逻辑统一复用 `collector.output_filter`，避免重复配置与行为不一致。

## 并发、超时与重试
- 设备并发度：遵循采集器全局并发档位 `collector.concurrency_profile`，依据 `collector.concurrency_profiles` 映射取值（示例：S={8,32}, M={16,64}, L={32,128}, XL={64,256}）。如未配置档位，回退到 `collector.concurrent`。档位中的 `threads` 将覆盖 SSH 会话上限。
- 超时：请求级 `timeout` 上限覆盖；命令执行上下文使用该值（或平台默认计算）。
- 重试：命令级 `retry.count` 次，指数退避或固定 `backoff_ms`；避免重复写入同一对象（命名幂等）。

## 安全与合规
- 屏蔽敏感信息：日志/对象中不包含密码；执行时避免密码在终端输出回显。
- 提权策略：与采集一致（交互优先、回退逐条 `sudo`），但备份服务仅复用执行器，不自研新的提权逻辑。
- 访问控制：MinIO 使用独立凭据与备份专用 bucket；本地存储目录权限限制。

## 监控与可观测性
- 统一日志：开始/结束、设备级耗时、对象写入结果、错误明细。
- 指标建议：设备数、命令数、成功率、平均耗时、对象大小分布。

## 开发计划（后续迭代）
1. 新增文件：
   - `internal/service/backup_types.go`（请求/响应/模型）。
   - `internal/service/backup.go`（服务实现）。
   - `internal/service/exec_adapter.go`（命令执行适配器）。
   - `internal/service/storage_writer.go`（本地/MinIO 写入实现）。
   - `api/handler/backup.go` 与路由注册。
2. 配置：在 `configs/config.yaml` 增加 `backup` 节点（或复用 `storage.minio`）。
3. 文档：本设计文档；新增 `docs/api/backup.md`（接口说明与示例）。
4. 测试：集成测试（本地与 MinIO 两后端）；并发与超时；错误用例；对象键幂等。
5. 性能：大输出时的分块/压缩策略（可选后续优化）。

## 兼容性与影响评估
- 对既有采集服务与 SSH 客户端零改动；仅新增备份相关代码与配置键。
- 若复用 `storage.minio`，无配置重复；如新增 `backup` 节点，需在加载配置时增加读取但不影响现有逻辑。

## 示例对象命名
- 本地：`./backups/system_auto/test-device-01/20251016/T-2001/show-running-config.txt`
- MinIO：`<bucket>/backups/system_auto/test-device-01/20251016/T-2001/show-running-config.txt`