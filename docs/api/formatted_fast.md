# 快速数据格式化接口（api/v1/formatted/fast）

面向单台设备的快速格式化能力，复用现有登录与命令采集能力，低耦合，仅返回格式化 JSON，不强制写入 MinIO。

## 使用场景
- 单设备临时检查或调试 FSM 模板
- 在批量格式化之外，快速验证采集与解析效果

## 请求
- 方法：`POST`
- 路径：`/api/v1/formatted/fast`
- Content-Type：`application/json`

### 入参结构
```
{
  "task_id": "CC-20251016-TASK-1",
  "task_name": "custom-batch-check",
  "retry_flag": 2,            // 仅用于采集重试；解析只进行一次
  "timeout": 15,               // 单次登录执行的整体超时（秒）
  "fsm_templates": [           // 可选，按平台与命令提供 FSM 模板
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        {"cli_name": "show version", "fsm_value": "...模板内容..."}
      ]
    }
  ],
  "device": [                  // 目前仅支持一台设备（数组便于扩展）
    {
      "device_ip": "10.200.40.201",
      "device_port": 22,
      "device_name": "test-out-r1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "eccom123",
      "password": "Eccom@12345",
      "enable_password": "Eccom@12345",
      "cli": "show version"   // 或使用 cli_list 传多条命令
      // "cli_list": ["show running-config", "show version"]
    }
  ]
}
```

说明：
- `retry_flag`：采集失败时的重试次数（总尝试次数=retry_flag+1）
- `timeout`：本次采集任务的整体超时时间（秒）
- `fsm_templates`：可选。用于解析的模板集合，按 `device_platform + cli_name` 选择
- `cli`/`cli_list`：支持单条或多条命令；当二者同时存在时以 `cli` 为主

## 响应
```
{
  "code": "SUCCESS",
  "message": "快速格式化处理完成",
  "task_id": "...",
  "date_time": "YYYYMMDD_HHMMSS",
  "result": "success | collect_failed | formatted_failed",
  "device": {
    "device_ip": "...",
    "device_name": "...",
    "device_platform": "..."
  },
  "raw": [
    {
      "command": "show version",
      "raw_output": "... 原始输出 ...",
      "format_output": null,
      "error": "",
      "exit_code": 0,
      "duration_ms": 200
    }
  ],
  "formatted_json": {
    "show version": {"parsed": [ /* 解析结果数组 */ ]}
  }
}
```

### 字段含义
- `result`
  - `success`：至少有一条命令解析得到非空 `parsed` 数据
  - `collect_failed`：采集阶段失败或原始输出全部为空
  - `formatted_failed`：采集成功但全部命令解析得到的 `parsed` 为空
- `raw`：原始采集结果视图，包含命令、输出、错误、退出码、耗时
- `formatted_json`：按命令聚合的解析结果。每条命令对应一个对象，包含字段 `parsed`

## 行为与约束
- 复用平台级预命令（如 `enable` 与关闭分页），统一交互层自动过滤掉这些预命令的输出，确保 `raw` 仅包含用户命令结果；格式化服务不再额外执行过滤
- 解析流程：
  - 按平台与命令选择对应的 FSM 模板（多模板时按顺序尝试）
  - 优先使用 TextFSM 语义引擎（状态机、Required、Filldown、List、IgnoreCase）；若不命中则回退到简单逐行匹配
  - 生成 `parsed` 为结构化变量映射数组（必要时包含 `line` 或聚合字段）
- 不进行 MinIO 写入；只返回内存中的结果（符合“低耦合”要求）

## TextFSM 支持
- 语法要点：
  - `Value [Required] [Filldown] [List] NAME (REGEX)`：变量定义与选项
  - `Start` / `State NAME`：状态定义
  - 规则行：`^... ${VAR} ... -> Continue | Record | NextState`
  - `Options IgnoreCase`：可选，大小写不敏感
- 语义说明：
  - `Record`：产出一条记录（若 `Required` 变量缺失则跳过）
  - `Continue`：仅更新上下文，不产出记录
  - `NextState`：状态切换；可配合 `Record`（`-> Record NextState`）使用
  - `Filldown`：变量值在后续记录中延续，除非被新匹配覆盖
  - `List`：同名变量聚合为数组
- 回退策略：
  - 若模板中没有显式 `Record`，但规则匹配到了变量，默认产出一条记录以便快速验证
  - 若 TextFSM 未命中，则回退到简单占位符/逐行正则匹配
- 模板在 JSON 中的转义：
  - `\S`、`\d` 等需双反斜杠，如 `\\S`、`\\d`
  - 换行使用 `\n`；可在字符串中按自然格式编写模板然后转义

示例模板（提取主机名与运行时长，并立即记录）：
```
Value Required HOSTNAME (\S+)
Value Required UPTIME (.+)

Start
  ^\s*${HOSTNAME}\s+uptime\s+is\s+${UPTIME} -> Record
```

示例请求文件：`payload_formatted_fast_textfsm_record.json`

## 示例
请求示例文件：`payload_formatted_fast_demo.json`
```
{
  "task_id": "DEMO-20251017-FAST-01",
  "task_name": "formatted-fast-demo",
  "retry_flag": 1,
  "timeout": 30,
  "fsm_templates": [
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        {"cli_name": "show version", "fsm_value": "fsm SV v1"}
      ]
    }
  ],
  "device": [
    {
      "device_ip": "139.196.196.96",
      "device_port": 21201,
      "device_name": "test-out-r1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "eccom123",
      "password": "Eccom@12345",
      "enable_password": "Eccom@12345",
      "cli": "show version"
    }
  ]
}
```

调用：
```
curl -s -X POST http://localhost:18000/api/v1/formatted/fast \
  -H 'Content-Type: application/json' \
  --data @payload_formatted_fast_demo.json
```

## 返回判定逻辑（简化）
- 若采集失败或原始输出为空：`result = collect_failed`
- 若采集成功但所有命令 `parsed` 为空：`result = formatted_failed`
- 其他情况：`result = success`

## 版本与演进
- v1：初始版本，仅内存返回结果；支持单设备、多命令；模板为简单正则/字面匹配
- v1.1：引入 TextFSM 引擎（状态机、Required、Filldown、List、IgnoreCase）并保留回退路径；当无 `Record` 时为开发/调试友好自动产出记录
- 未来计划：
  - 支持多设备（保持轻耦合）
  - 增强 FSM 模板语义（字段映射、分组命名）
  - 增加可选的 MinIO 写入（按需开启）