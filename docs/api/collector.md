# 采集接口 API 文档

本文档描述采集接口的输入/输出参数、字段含义、错误设计，以及系统支持的设备类型与内置格式化命令列表。

## 输入参数
- `task_id`：任务唯一标识，必填。
- `task_name`：任务名称，选填。
- `collect_origin`：采集来源，取值 `system` 或 `customer`，必填。
  - `system`：系统内置任务，按设备平台执行系统内置的采集命令。
  - `customer`：用户自定义任务，按传入的 `cli_list` 执行。
- `device_ip`：设备 IP，必填。
- `device_name`：设备名称，选填。
- `device_platform`：设备平台，选填；当 `collect_origin=system` 时必须提供。
- `collect_protocol`：采集协议，当前支持 `ssh`，选填；为空时默认按 SSH 处理。
- `port`：SSH 端口，选填；未提供或非法时默认 `22`。
- `user_name`：登录用户名，必填。
- `password`：登录密码，必填。
- `enable_password`：特权/enable 密码，选填。
- `cli_list`：命令列表，可为空/一个/多个命令。
  - 当 `collect_origin=system` 时可为空，系统将按平台的内置命令执行。
  - 当 `collect_origin=customer` 时可为空，此时不会执行任何命令，返回空结果。
- `retry_flag`：重试次数，选填；为空时读取交互插件默认设置。
- `timeout`：超时时间（秒），选填；为空时读取交互插件默认设置。

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

## collect_origin 说明
- `system`：
  - 根据 `device_platform` 调度系统内置的采集命令（见下文的“系统内置格式化命令”）。
  - 执行完成后输出 `raw_output`，并在可格式化时同时输出 `format_output`。
- `customer`：
  - 按 `cli_list` 执行用户指定命令；当命令满足对应平台的格式化条件时，返回 `format_output`。
  - 当 `cli_list` 为空时，返回空的 `result`。

## 错误设计
- 参数校验错误（HTTP 400）：
  - `task_id`、`device_ip`、`user_name`、`password` 缺失或为空。
  - `collect_protocol` 非法（目前仅支持 `ssh`）。
  - `collect_origin=system` 且 `device_platform` 为空。
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

## 系统内置支持格式化的命令（按设备类型）
- `cisco_ios`
  - `show run`
  - `show version`
  - `show interfaces`
- `huawei_s`
  - `display current-configuration`
  - `display version`
- `huawei_ce`
  - `display current-configuration`
  - `display version`
- `h3c_s`
  - `display current-configuration`
  - `display version`
- `h3c_sr`
  - `display current-configuration`
  - `display version`
- `h3c_msr`
  - `display current-configuration`
  - `display version`

> 注：上述命令清单来自各平台的采集插件 `SystemCommands()` 定义，可根据后续平台能力扩展。

## 示例

### 请求（system）
```json
{
  "task_id": "t-1001",
  "task_name": "daily-inventory",
  "collect_origin": "system",
  "device_ip": "10.0.0.1",
  "device_name": "sw-core-1",
  "device_platform": "huawei_s",
  "collect_protocol": "ssh",
  "user_name": "admin",
  "password": "p@ssw0rd",
  "enable_password": "enable123",
  "cli_list": [],
  "retry_flag": null,
  "timeout": null
}
```

### 请求（customer）
```json
{
  "task_id": "t-1002",
  "task_name": "quick-check",
  "collect_origin": "customer",
  "device_ip": "10.0.0.2",
  "device_name": "ios-edge-1",
  "device_platform": "cisco_ios",
  "collect_protocol": "ssh",
  "user_name": "ops",
  "password": "xxxx",
  "cli_list": ["show version", "show interfaces"],
  "retry_flag": 2,
  "timeout": 60
}
```

### 响应（示例）
```json
{
  "task_id": "t-1002",
  "success": true,
  "error": "",
  "timestamp": "2025-10-13T10:20:30Z",
  "result": [
    {
      "command": "show version",
      "exit_code": 0,
      "duration_ms": 1200,
      "error": "",
      "raw_output": "Cisco IOS Software, ...",
      "format_output": {
        "platform": "cisco_ios",
        "table": "device_version",
        "data": {"version": "15.2(4)E", "model": "C2960X"}
      }
    },
    {
      "command": "show interfaces",
      "exit_code": 0,
      "duration_ms": 2300,
      "error": "",
      "raw_output": "GigabitEthernet0/1 is up, ...",
      "format_output": {}
    }
  ]
}
```

## 说明
- 当 `format_output` 为空对象或空数组时，表示该命令未匹配到格式化规则或无法解析。
- `retry_flag` 与 `timeout` 为空时使用交互插件默认值；平台可自行覆盖默认值。