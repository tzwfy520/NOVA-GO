# SSH采集器专业版 (SSH Collector Pro)

高性能、可扩展的 SSH 设备采集与模拟系统。支持批量并发、重试控制、结果格式化、设备模拟与平台适配，为网络与系统运维提供稳定、高效的采集能力。

## 功能概览
- 采集执行：批量任务、并发队列、重试与超时控制、任务取消
- 连接池：可配置连接超时、会话上限、KeepAlive，复用稳定
- API接口：统一 RESTful，含批量自定义采集、状态查询与格式化接口
- 结果处理：原始输出与格式化视图并存，便于二次处理
- 模拟器：可本地模拟设备，支持命令匹配（精确/模糊/前缀）与提示符
- 日志与追踪：结构化日志、任务时长、错误追踪
- 配置管理：YAML 配置，支持并发档位与平台默认值

## 架构与模块
- 核心服务：`api/router`（HTTP 路由） + `api/handler`（业务入口）
- 采集服务：`internal/service/collector.go`（批量采集与重试）
- 交互层：`internal/service/interact_basic.go`（统一 SSH 交互与预命令过滤）
- SSH连接池：`pkg/ssh`（连接与会话资源管理）
- 模拟器：`simulate/Simulate.go`（命名空间/设备/命令文件）
- 配置与模型：`internal/config`、`internal/model`
- 文档：`docs/api/*.md`、`docs/simulate.md`

## 快速开始
1) 构建与运行（本地开发）
- 构建：`go build -o dist/sshcollector ./cmd/server`
- 启动：`./dist/sshcollector -config configs/dev.yaml`
- 健康检查：`GET /health`（默认端口 `18000`）

2) 构建脚本
- `./scripts/build.sh`（打包产物）

3) 目录约定
- 配置：`configs/dev.yaml | prod.yaml`
- 日志：`logs/`
- 模拟：`simulate/`（含 `simulate.yaml` 与命令文件目录）

## 配置要点
- 并发档位（推荐）：在 `configs/*.yaml` 中为不同机器规格设置安全并发
  - `collector.concurrency_profile: S|M|L|XL`
  - 档位会覆盖 `collector.concurrent` 与 SSH `MaxSessions`
- SSH 连接：`ssh.timeout`、`ssh.connect_timeout`、`ssh.keep_alive_interval`、`ssh.max_sessions`
- 重试：`collector.retry_flags`（作为请求 `retry_flag` 的默认回退）

示例（并发档位）：
```
collector:
  concurrency_profile: "S"
  concurrency_profiles:
    S: { concurrent: 8,  threads: 32 }
    M: { concurrent: 16, threads: 64 }
  concurrent: 5
```

## API 细节
- 基本约定：
  - 所有数据接口使用 `application/json`；跨域默认允许，支持 `OPTIONS` 预检
  - 建议在请求头添加 `X-Request-ID`（可选），用于端到端追踪；服务会在响应头回写同名字段
  - 统一响应封装：`{ "code": "SUCCESS|PARTIAL_SUCCESS|...", "message": "...", "data": [...], "total": N }`

- 路由与方法（`/api/v1`）：
  - 系统与健康：
    - `GET /health`（健康检查，服务未运行返回 `503` 与 `SERVICE_UNAVAILABLE`）
    - `GET /collector/stats`（采集器统计，含 `running`、`active_tasks`、`max_workers`、`busy_workers`、`ssh_pool`）
  - 采集（批量）：
    - `POST /collector/batch`（通用批量；旧版保留）
    - `POST /collector/batch/custom`（自定义批量；按 `devices[].cli_list` 执行）
    - `POST /collector/batch/system`（系统批量；按 `device_list[].cli_list` 执行）
    - `GET /collector/task/:task_id/status`（任务状态：`task_id`、`status`、`start_time`、`duration`；任务不存在返回 `404 TASK_NOT_FOUND`）
    - `POST /collector/task/:task_id/cancel`（取消任务，若任务不存在返回 `404`）
  - 格式化：
    - `POST /formatted/batch`（批量格式化；与采集结果结合）
    - `POST /formatted/fast`（快速格式化；参见 `docs/api/formatted_fast.md`）
  - 备份：
    - `POST /backup/batch`（批量备份）
  - 部署：
    - `POST /deploy/fast`（快速部署与状态检查）
  - 设备管理：
    - `POST /devices`、`GET /devices`、`GET /devices/:id`、`PUT /devices/:id`、`DELETE /devices/:id`、`POST /devices/:id/test`

- 请求体字段（采集核心）：
  - 顶层：`task_id`（必填）、`task_name`、`retry_flag`（重试次数，≥0）、`task_timeout`（秒）
  - 设备（custom/batch → `devices[]`；system/batch → `device_list[]`）：
    - `device_ip`（必填）、`device_port`（默认 `22`）、`device_name`、`device_platform`（system 必填）、`collect_protocol`（默认 `ssh`）
    - `user_name`（必填）、`password`（必填）、`enable_password`（选填）
    - `cli_list`（命令数组，按顺序执行）、`device_timeout`（秒）

- 响应体（每台设备）：
  - `device_ip`、`port`、`device_name`、`device_platform`、`task_id`
  - `success`（布尔）、`error`（字符串）、`duration_ms`（整型）、`timestamp`
  - `results`：数组，元素为 `{ command, raw_output, format_output, error, exit_code, duration_ms }`
  - 批量接口顶层 `code`：
    - `SUCCESS`：全部设备成功
    - `PARTIAL_SUCCESS`：部分或全部失败（包含 `success=false` 的设备项）

- 并发与队列说明：
  - 批内并发度由服务端 `max_workers` 限制，实际并发为 `min(max_workers, 设备数)`；可在 `GET /collector/stats` 中查看
  - 队列等待超时由有效 `task_timeout` 推导（默认 30s）；超过后返回队列超时错误

- 错误码与状态：
  - `INVALID_PARAMS`（400）：JSON解析失败或必填字段缺失
  - `MISSING_TASK_ID`（400）：任务ID为空
  - `EMPTY_DEVICES`/`TOO_MANY_DEVICES`（400）：设备列表为空或超过上限（200）
  - `TASK_NOT_FOUND`（404）：任务不存在（状态查询/取消）
  - `SERVICE_UNAVAILABLE`（503）：服务未运行（健康检查）
  - 采集阶段错误（如握手/认证失败）以设备级 `error` 字段呈现；批量接口总体仍为 `200`，通过 `code=PARTIAL_SUCCESS` 表示部分/全部失败

- 示例：状态查询
```
curl -sS 'http://localhost:18000/api/v1/collector/task/T-quick-001-1/status'
# 响应示例
{
  "task_id": "T-quick-001-1",
  "status": "running|success|failed|cancelled",
  "start_time": "2025-10-20T06:20:00Z",
  "duration": "2.345678s"
}
```

- 示例：统计信息
```
curl -sS 'http://localhost:18000/api/v1/collector/stats'
# 响应示例（简化）
{
  "code": "SUCCESS",
  "message": "获取统计信息成功",
  "data": {
    "running": true,
    "active_tasks": 1,
    "max_workers": 4,
    "busy_workers": 1,
    "ssh_pool": { "active": 0, "idle": 0 }
  }
}
```

更多细节与完整示例请参见 `docs/api/collector.md` 与 `docs/api/formatted_fast.md`。

## 采集 API
- 批量自定义采集：`POST /api/v1/collector/batch/custom`
- 请求字段（关键）：
  - `device_ip`、`device_port`、`user_name`、`password`
  - `cli_list`（命令数组）
  - `retry_flag`（可选，优先于平台默认与全局回退）
  - `task_timeout`、`device_timeout`（可选）

示例（针对本地模拟 `huawei-01`）
```
curl -sS -X POST 'http://localhost:18000/api/v1/collector/batch/custom' \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "T-quick-001",
    "retry_flag": 0,
    "timeout": 30,
    "devices": [ {
      "device_ip": "127.0.0.1",
      "device_port": 22001,
      "device_name": "huawei-01",
      "device_platform": "huawei",
      "collect_protocol": "ssh",
      "user_name": "huawei-01",
      "password": "nova",
      "cli_list": [ "dis ver", "display interface brief" ]
    } ]
  }'
```

返回包含每台设备的 `success`、`duration_ms`、`results`（命令与输出）与错误信息。

## 模拟器说明（simulate）
- 配置文件：`simulate/simulate.yaml`
  - 命名空间示例：`namespace.default.port: 22001`、`max_conn: 5`
  - 设备类型：`device_type.huawei`（提示符等）
  - 设备名映射：`device_name.huawei-01.device_type: huawei`
- 登录约定：
  - 用户名为设备名（如 `huawei-01`），密码固定为 `nova`
- 命令匹配：
  - 精确匹配：`display version` → `display_version.txt`
  - 归一化匹配：空格→下划线
  - 模糊匹配：大小写不敏感、包含匹配（空格/下划线宽松）
  - 前缀匹配：按词顺序前缀（如 `dis ver` → `display version`、`dis int bri` → `display interface brief`）
  - 多匹配提示：`display` → 返回候选列表
- 命令文件目录：`simulate/namespace/<ns>/<device>/`
  - 例如：`display_version.txt`、`display_interface_brief.txt`
  - 可选：`supported_commands.txt`（优先用于候选集合）
- 并发上限：`max_conn` 控制每命名空间的同时连接数，超过将拒绝握手

## 并发与重试实践
- 并发上限（模拟器）：`simulate.yaml` 中 `max_conn`（默认 `5`）
  - 若需同时 10 请求，建议将其提高到 `10` 以上并重启模拟服务
- 请求重试：接口中设置 `retry_flag: 2|3`，可降低瞬时握手失败带来的误差
- 节流与抖动：并发尖峰时建议在发起端引入 50–100ms 的微抖动

## 日志与排障
- 任务日志：记录任务时长、重试次数与错误信息
- 常见错误：`ssh: handshake failed`（并发上限/瞬时拥塞）、认证失败（密码或用户名不匹配）
- 观察点：`duration_ms` 分布、失败样例聚类

## 开发与测试
- 单元测试：`go test -v ./...`
- Lint：`golangci-lint run`
- 示例程序：`cmd/cli/test_batch_custom.go`（批量采集示例）

## 贡献指南
- 提交类型：`feat`/`fix`/`docs`/`refactor`/`test`/`chore`/`style`
- 适配请求：请在 Issue 中提供设备型号、平台版本与若干典型命令输出样本，便于快速集成与验证

## 许可证
- 本项目采用 MIT License

## 未来计划
- 增加设备适配类型（可提出适配请求，需要提出方配合）
- 增加内置解析能力
- API/SNMP 协议对接能力

## 路线图
- v1.1.0（计划）：Web 管理界面、更多设备类型支持、插件系统、集群模式
- v1.2.0（计划）：异常检测、自动化运维脚本、移动端应用、多租户支持