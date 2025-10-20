# 采集接口 API 文档

本文档描述采集接口的输入/输出参数、字段含义、错误设计，以及系统支持的设备类型与内置格式化命令列表。

## 输入参数
- `task_id`：任务唯一标识，必填。
- `task_name`：任务名称，选填。
- `device_ip`：设备 IP，必填。
- `device_name`：设备名称，选填。
- `device_platform`：设备平台，选填；系统批量接口中为必填。
- `collect_protocol`：采集协议，当前支持 `ssh`，选填；为空时默认按 SSH 处理。
- `device_port`：SSH 端口，选填；未提供或非法时默认 `22`。
- `user_name`：登录用户名，必填。
- `password`：登录密码，必填。
- `enable_password`：特权/enable 密码，选填。
  - 用途：当平台需要进入特权模式时使用（如 Cisco 的 `enable`）。未提供或平台不需要特权时忽略。
- `devices[].cli_list`：自定义批量接口的设备级命令列表，可为空/一个/多个命令。
- `device_list[].cli_list`：系统批量接口的设备级命令列表，可为空/一个/多个命令。
 - `retry_flag`：重试次数，选填；为空时使用系统内置交互默认值。
 - `timeout`：超时时间（秒），选填；为空时使用系统内置交互默认值。

## 输出参数
- `task_id`：任务标识。
- `success`：任务整体是否成功（所有命令均成功）。
- `error`：错误信息（当存在错误时）。
- `timestamp`：服务端时间戳。
- `result`：命令执行结果数组，每条包含：
  - `command`：执行的命令。
  - `exit_code`：进程退出码（SSH命令执行结果）。
  - `duration_ms`：执行耗时（毫秒）。
  - `error`：本条命令的错误信息（如有）。
  - `raw_output`：原始输出文本。
  - `format_output`：格式化输出（JSON 对象或数组）；当无法格式化时为空。

说明：历史字段 `output` 已重命名为 `raw_output`，新增 `format_output` 字段用于承载格式化结果。

## 采集模式说明
- 不再通过 `collect_origin` 标识采集模式。
- 采集模式由接口路径隐式决定：
  - `/api/v1/collector/batch/custom` → 自定义采集模式（按 `cli_list` 执行）。
  - `/api/v1/collector/batch/system` → 系统批量采集模式（仅执行设备条目中的 `cli_list`）。

## 错误设计
- 参数校验错误（HTTP 400）：
  - `task_id`、`device_ip`、`user_name`、`password` 缺失或为空。
  - `collect_protocol` 非法（目前仅支持 `ssh`）。
- 系统批量接口中 `device_platform` 为空。
  - `timeout` 超过上限（建议 ≤ 300 秒）。
  - `retry_flag` 为负数。
- 资源/插件错误（HTTP 404）：指定的 `device_platform` 未找到对应插件。
- 连接与认证错误（HTTP 502）：SSH 建立连接失败、认证失败。
- 执行超时（HTTP 504）：命令整体或单条命令超时（超时可按交互插件默认或请求参数控制）。
- 部分成功（HTTP 200，`success=false`，附 `result`）：
  - 任务整体返回成功状态码，但某些命令失败；`result` 中逐条列出失败原因。

## 支持设备类型
- `cisco_ios`
- `huawei_s`
- `huawei_ce`
- `h3c_s`
- `h3c_sr`
- `h3c_msr`

## 重要说明（系统批量采集）
- `device_platform` 仍为必填，用于交互参数与提示符匹配。
- 不再注入平台默认命令；仅执行用户在 `device_list[].cli_list` 中提供的命令。
- 统一交互层（InteractBasic）负责预命令注入（如 `enable`、关闭分页）与结果过滤，服务层不再重复处理。

> 重要：原单设备接口已下线，请使用批量接口 `/api/v1/collector/batch/custom` 或 `/api/v1/collector/batch/system`。

## 说明
- 当 `format_output` 为空对象或空数组时，表示该命令未匹配到格式化规则或无法解析。
- `retry_flag` 与 `timeout` 为空时使用系统内置交互默认值；不再依赖外部插件覆盖。

## 问题诊断（Problems & Diagnostics）
- System批量接口参数要求：`device_platform` 必填；若未提供 `cli_list`，将返回空结果。建议为每台设备提供至少一条命令。
- 错误提示识别：可通过配置 `collector.interact.error_hints` 扩展错误前缀，默认大小写不敏感并自动去空格。示例：`invalid input`、`unrecognized command`。
- Cisco特权失败：若结果包含 `privileged mode not entered (#)`，需在请求中提供 `enable_password` 或确保用户具备特权；系统会尝试执行 `enable` 并取消分页。
- 提示符识别异常：检查设备平台与提示符后缀。平台默认后缀示例：Cisco `#`/`>`，Huawei/H3C `]`。如需自定义请在代码中调整 `getPlatformDefaults`。
- 连接失败：检查 `device_ip` 与 `device_port`、认证参数、以及 `ssh.timeout` 设置；可直接使用系统命令进行手动SSH测试验证。
- 输出过滤：默认移除分页提示（`collector.output_filter`），如需保留原始提示请调整配置。

---

## 批量调用

为满足批量场景与系统/自定义任务拆分，新增两类批量接口：

### 自定义采集批量接口
- 路径：`POST /api/v1/collector/batch/custom`
- 输入参数：
  - 统一参数：`task_id`、`task_name`、`retry_flag`、`timeout`
  - 设备参数（按设备数量组织为数组 `devices`）：`device_ip`、`device_name`、`device_platform`、`collect_protocol`、`user_name`、`password`、`device_port`、`cli_list`
 - 输出参数：按设备组织输出，每个设备包含设备标识与该设备对应的采集执行结果；新增 `port` 字段标识设备登陆端口。

示例请求（custom/batch）：
```json
{
  "task_id": "T-2001",
  "task_name": "custom-batch-check",
  "retry_flag": 2,
  "timeout": 60,
  "devices": [
    {
      "device_ip": "10.0.0.2",
      "device_name": "ios-edge-1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "ops",
      "password": "xxxx",
      "enable_password": "xxxx",
      "device_port": 22,
      "cli_list": ["show version", "show running-config"]
    },
    {
      "device_ip": "10.0.0.3",
      "device_name": "sw-agg-1",
      "device_platform": "huawei_s",
      "collect_protocol": "ssh",
      "user_name": "ops",
      "password": "yyyy",
      "device_port": 2222,
      "cli_list": ["display version", "display current-configuration"]
    }
  ]
}
```

示例响应（custom/batch）：
```json
{
  "code": "SUCCESS",
  "message": "自定义批量任务执行完成",
  "total": 2,
  "data": [
    {
      "device_ip": "10.0.0.2",
      "port": 22,
      "device_name": "ios-edge-1",
      "device_platform": "cisco_ios",
      "task_id": "T-2001-1",
      "success": true,
      "results": [ /* 与单次接口 result 结构一致 */ ],
      "error": "",
      "duration_ms": 3400,
      "timestamp": "2025-10-13T10:20:30Z"
    },
    {
      "device_ip": "10.0.0.3",
      "port": 2222,
      "device_name": "sw-agg-1",
      "device_platform": "huawei_s",
      "task_id": "T-2001-2",
      "success": true,
      "results": [ /* 与单次接口 result 结构一致 */ ],
      "error": "",
      "duration_ms": 2100,
      "timestamp": "2025-10-13T10:20:31Z"
    }
  ]
}
```

> 说明：批量任务下的每个设备执行会自动生成子任务ID（示例为 `T-2001-1`、`T-2001-2`），用于区分与追踪；返回结果按设备维度组织。

### 系统批量采集接口
- 路径：`POST /api/v1/collector/batch/system`
- 输入参数：
  - 统一参数：`task_id`、`task_name`、`retry_flag`、`timeout`
  - 设备参数数组 `device_list`：`device_ip`、`device_name`、`device_platform`（必填）、`collect_protocol`、`user_name`、`password`、`cli_list`
  - `cli_list`：执行命令列表，未提供时不执行任何命令，返回空结果。
- 输出参数：按设备组织输出，每个设备包含设备标识与该设备对应的系统批量采集执行结果。

示例请求（system/batch）：
```json
{
  "task_id": "T-3001",
  "task_name": "system-batch-inventory",
  "retry_flag": 1,
  "timeout": 45,
  "device_list": [
    {
      "device_ip": "10.0.1.10",
      "device_name": "core-sw-1",
      "device_platform": "huawei_s",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "123456",
      "enable_password": "123456",
      "cli_list": ["display version", "display current-configuration"]
    },
    {
      "device_ip": "10.0.1.20",
      "device_name": "edge-ios-1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "ops",
      "password": "abcd",
      "enable_password": "abcd",
      "cli_list": ["show version", "show running-config"]
    }
  ]
}
```

示例响应（system/batch）：
```json
{
  "code": "SUCCESS",
  "message": "系统批量任务执行完成",
  "total": 2,
  "data": [
    {
      "device_ip": "10.0.1.10",
      "port": 22,
      "device_name": "core-sw-1",
      "device_platform": "huawei_s",
      "task_id": "T-3001-1",
      "success": true,
      "results": [ /* 与单次接口 result 结构一致 */ ],
      "error": "",
      "duration_ms": 2800,
      "timestamp": "2025-10-13T10:21:00Z"
    },
    {
      "device_ip": "10.0.1.20",
      "port": 22,
      "device_name": "edge-ios-1",
      "device_platform": "cisco_ios",
      "task_id": "T-3001-2",
      "success": true,
      "results": [ /* 与单次接口 result 结构一致 */ ],
      "error": "",
      "duration_ms": 3200,
      "timestamp": "2025-10-13T10:21:02Z"
    }
  ]
}
```

> 说明：`collect_origin` 不再作为批量接口入参传递，由接口路径隐式决定（`/batch/custom` → `customer`，`/batch/system` → `system`）。系统批量接口要求 `device_platform` 必填，且仅执行设备条目中的 `cli_list`；不再注入平台默认命令。

> 使用提示：两类批量接口均支持 `enable_password`，当目标平台需要进入特权模式时将自动尝试（例如执行 `enable` 并输入口令）。若平台不需要或用户已具备特权，提供该字段不会影响结果。

### 兼容性
- 原单设备接口已移除；通用批量接口（`POST /api/v1/collector/batch`）保留，建议迁移到拆分后的批量接口以获得更清晰的语义。

---

## 平台扩展说明（高级）
- 系统允许在配置中扩展新的 `device_platform`，接口不限制具体取值；未内置的平台将使用通用交互默认。
- 建议在配置文件中为新平台设置：提示符后缀（`prompt_suffixes`）、分页关闭命令（`disable_paging_cmds`）、是否需要特权（`enable_required`）、特权进入命令（`enable_cli`）、以及分页/提示的自动交互（`auto_interactions`）。
- 如需扩展，请参考 `configs/config.yaml` 的 `collector.device_defaults` 与 `collector.interact.auto_interactions` 配置段示例。
- 交互时序参数（可选）：可在 `collector.device_defaults.<platform>` 下设置以下键以覆盖默认行为：
  - `command_interval_ms`：两条命令之间的发送间隔，避免设备限流（默认不等待）。
  - `command_timeout_sec`：单条命令最长等待时间（默认 30）。
  - `quiet_after_ms`：在已收到输出后连续静默视为完成的阈值（默认 800）。
  - `quiet_poll_interval_ms`：静默检测轮询间隔（默认 250）。
  - `enable_password_fallback_ms`：未检测到 enable 密码提示时的回退发送延迟（默认 1500）。
  - `prompt_inducer_interval_ms`：初始提示符诱发器发送 CRLF 的间隔（默认 1000）。
  - `prompt_inducer_max_count`：初始提示符诱发器的最大次数（默认 12）。
  - `exit_pause_ms`：退出命令间的停顿时间（默认 150）。