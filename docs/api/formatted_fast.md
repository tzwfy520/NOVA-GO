# 快速数据格式化接口（api/v1/formatted/fast）

面向单台设备的快速格式化能力，复用现有登录与命令采集能力，低耦合，仅返回格式化 JSON，不强制写入 MinIO。

## 概述

快速格式化接口是一个轻量级的数据处理接口，专为单设备快速验证和调试场景设计。该接口复用了现有的设备登录和命令采集能力，支持 TextFSM 模板解析，提供实时的格式化结果，无需存储到 MinIO。

### 核心特性

- **单设备处理**：专注于单台设备的快速格式化
- **实时响应**：不进行存储操作，直接返回格式化结果
- **TextFSM 支持**：完整支持 TextFSM 语法和语义
- **低耦合设计**：复用现有采集能力，独立的格式化逻辑
- **调试友好**：提供原始数据和格式化结果对比

## 使用场景
- 单设备临时检查或调试 FSM 模板
- 在批量格式化之外，快速验证采集与解析效果
- 开发和测试阶段的模板验证
- 故障排查和数据分析

## 请求
- 方法：`POST`
- 路径：`/api/v1/formatted/fast`
- Content-Type：`application/json`

### 入参结构

```json
{
  "task_id": "CC-20251016-TASK-1",
  "task_name": "custom-batch-check",
  "retry_flag": 2,
  "task_timeout": 15,
  "fsm_templates": [
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        {"cli_name": "show version", "fsm_value": "...模板内容..."}
      ]
    }
  ],
  "device": [
    {
      "device_ip": "10.200.40.201",
      "device_port": 22,
      "device_name": "test-out-r1",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "eccom123",
      "password": "Eccom@12345",
      "enable_password": "Eccom@12345",
      "cli": "show version",
      "device_timeout": 30
    }
  ]
}
```

### 参数说明

#### 任务级参数
- `task_id`：任务唯一标识符，必填
- `task_name`：任务名称，可选，用于标识和日志记录
- `retry_flag`：采集重试次数，可选，默认为1，仅用于采集重试，解析只进行一次
- `task_timeout`：整体任务超时时间（秒），可选，默认为30秒

#### FSM模板参数
- `fsm_templates`：FSM模板数组，可选
  - `device_platform`：设备平台类型，如 "cisco_ios"、"huawei_s" 等
  - `templates_values`：模板值数组
    - `cli_name`：命令名称，需与设备命令匹配
    - `fsm_value`：TextFSM模板内容

#### 设备参数
- `device`：设备数组，目前仅支持一台设备
  - `device_ip`：设备IP地址，必填
  - `device_port`：SSH端口，可选，默认22
  - `device_name`：设备名称，必填
  - `device_platform`：设备平台类型，必填
  - `collect_protocol`：采集协议，可选，默认"ssh"
  - `user_name`：登录用户名，必填
  - `password`：登录密码，必填
  - `enable_password`：特权模式密码，可选
  - `cli`：单条命令，与cli_list二选一
  - `cli_list`：命令列表，与cli二选一
  - `device_timeout`：设备级超时时间（秒），可选

说明：
- `retry_flag`：采集失败时的重试次数（总尝试次数=retry_flag+1）
- `timeout`：本次采集任务的整体超时时间（秒）
- `fsm_templates`：可选。用于解析的模板集合，按 `device_platform + cli_name` 选择
- `cli`/`cli_list`：支持单条或多条命令；当二者同时存在时以 `cli` 为主

## 响应

### 响应结构

```json
{
  "code": "SUCCESS",
  "message": "快速格式化处理完成",
  "task_id": "CC-20251016-TASK-1",
  "date_time": "20251016_111111",
  "result": "success",
  "device": {
    "device_ip": "10.200.40.201",
    "device_name": "test-out-r1",
    "device_platform": "cisco_ios"
  },
  "raw": [
    {
      "command": "show version",
      "output": "Cisco IOS Software...",
      "exit_code": 0,
      "duration_ms": 1234,
      "error": ""
    }
  ],
  "formatted_json": {
    "show_version": {
      "template": "fsm SV v1",
      "parsed": [
        {
          "hostname": "Router1",
          "version": "15.1(4)M4",
          "uptime": "1 year, 2 weeks, 3 days, 4 hours, 5 minutes"
        }
      ]
    }
  }
}
```

### 响应参数说明

#### 基础响应字段
- `code`：响应状态码，"SUCCESS" 表示成功
- `message`：响应消息描述
- `task_id`：任务ID，与请求中的task_id一致
- `date_time`：任务执行时间戳，格式为 YYYYMMDD_HHMMSS
- `result`：处理结果状态
  - `success`：采集和格式化都成功
  - `collect_failed`：设备采集失败
  - `formatted_failed`：采集成功但格式化失败

#### 设备信息
- `device`：设备基本信息
  - `device_ip`：设备IP地址
  - `device_name`：设备名称
  - `device_platform`：设备平台类型

#### 原始数据
- `raw`：原始命令执行结果数组
  - `command`：执行的命令
  - `output`：命令原始输出
  - `exit_code`：命令退出码，0表示成功
  - `duration_ms`：命令执行耗时（毫秒）
  - `error`：错误信息，成功时为空

#### 格式化数据
- `formatted_json`：格式化结果对象，以命令名为键
  - 每个命令的格式化结果包含：
    - `template`：使用的FSM模板标识
    - `parsed`：解析后的结构化数据数组

## 处理流程

### 执行步骤

1. **参数验证**：验证请求参数的完整性和有效性
2. **设备连接**：使用SSH协议连接到目标设备
3. **命令执行**：执行用户指定的命令，支持重试机制
4. **数据采集**：收集命令的原始输出和执行信息
5. **模板匹配**：根据设备平台和命令名称匹配FSM模板
6. **数据解析**：使用TextFSM引擎解析原始数据
7. **结果返回**：组装并返回格式化结果

### 重要特性

- **复用现有能力**：复用平台级预命令（如 `enable` 与关闭分页），统一交互层自动过滤掉这些预命令的输出，确保 `raw` 仅包含用户命令结果
- **智能解析**：按平台与命令选择对应的 FSM 模板（多模板时按顺序尝试），优先使用 TextFSM 语义引擎，若不命中则回退到简单逐行匹配
- **内存处理**：不进行 MinIO 写入，只返回内存中的结果，符合"低耦合"要求
- **错误容忍**：采集失败时支持重试，解析失败时提供详细的错误信息

## 错误处理

### 常见错误码

| 错误码 | 描述 | 解决方案 |
|--------|------|----------|
| INVALID_PARAMS | 请求参数无效 | 检查请求参数格式和必填字段 |
| SERVICE_NOT_READY | 格式化服务未初始化 | 检查服务状态，重启服务 |
| EXEC_FAILED | 快速格式化执行失败 | 检查设备连接和认证信息 |
| CONNECTION_FAILED | 设备连接失败 | 检查网络连通性和设备状态 |
| AUTH_FAILED | 认证失败 | 检查用户名和密码 |
| TIMEOUT | 执行超时 | 增加超时时间或检查设备响应 |

### 错误响应格式

```json
{
  "code": "EXEC_FAILED",
  "message": "快速格式化执行失败: connection timeout",
  "task_id": "CC-20251016-TASK-1",
  "date_time": "20251016_111111",
  "result": "collect_failed",
  "device": {
    "device_ip": "10.200.40.201",
    "device_name": "test-out-r1",
    "device_platform": "cisco_ios"
  },
  "raw": [],
  "formatted_json": {}
}
```

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

## 最佳实践

### 模板设计建议

1. **使用Required标记关键字段**：确保重要数据的完整性
2. **合理使用Filldown**：对于在多行中保持不变的字段
3. **List字段处理重复数据**：如接口列表、路由表等
4. **状态机设计**：复杂解析场景使用多状态切换
5. **错误容忍**：设计回退策略处理异常输出

### 性能优化

1. **合理设置超时时间**：根据设备响应特性调整
2. **模板复用**：相同平台的设备可共享模板
3. **命令优化**：避免执行耗时过长的命令
4. **并发控制**：单设备接口天然避免并发冲突

### 调试技巧

1. **查看原始输出**：通过raw字段检查采集结果
2. **模板验证**：先用简单模板验证连通性
3. **逐步完善**：从基础字段开始，逐步增加复杂度
4. **日志分析**：结合服务端日志排查问题

## 使用示例

### 基础示例

```json
{
  "task_id": "DEMO-20251017-FAST-01",
  "device": [
    {
      "device_ip": "192.168.1.1",
      "device_name": "router-01",
      "device_platform": "cisco_ios",
      "user_name": "admin",
      "password": "password",
      "cli": "show version"
    }
  ]
}
```

### 带FSM模板的示例

```json
{
  "task_id": "DEMO-20251017-FAST-02",
  "fsm_templates": [
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        {
          "cli_name": "show version",
          "fsm_value": "Value HOSTNAME (\\S+)\\nValue VERSION (\\S+)\\n\\nStart\\n  ^\\s*${HOSTNAME}\\s+uptime\\s+is\\s+.+ -> Continue\\n  ^Cisco\\s+IOS\\s+Software.*Version\\s+${VERSION} -> Record"
        }
      ]
    }
  ],
  "device": [
    {
      "device_ip": "192.168.1.1",
      "device_name": "router-01",
      "device_platform": "cisco_ios",
      "user_name": "admin",
      "password": "password",
      "cli": "show version"
    }
  ]
}
```

### 多命令示例

```json
{
  "task_id": "DEMO-20251017-FAST-03",
  "device": [
    {
      "device_ip": "192.168.1.1",
      "device_name": "router-01",
      "device_platform": "cisco_ios",
      "user_name": "admin",
      "password": "password",
      "cli_list": ["show version", "show ip interface brief"]
    }
  ]
}
```

## API调用示例

```bash
# 基础调用
curl -X POST http://localhost:18000/api/v1/formatted/fast \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "test-001",
    "device": [{
      "device_ip": "192.168.1.1",
      "device_name": "test-router",
      "device_platform": "cisco_ios",
      "user_name": "admin",
      "password": "password",
      "cli": "show version"
    }]
  }'

# 带模板调用
curl -X POST http://localhost:18000/api/v1/formatted/fast \
  -H 'Content-Type: application/json' \
  --data @payload_formatted_fast_demo.json
```

## 返回判定逻辑

- 若采集失败或原始输出为空：`result = collect_failed`
- 若采集成功但所有命令 `parsed` 为空：`result = formatted_failed`  
- 其他情况：`result = success`

## 注意事项

1. **单设备限制**：当前版本仅支持单台设备处理
2. **内存处理**：结果仅在内存中处理，不持久化存储
3. **模板转义**：JSON中的正则表达式需要正确转义
4. **超时设置**：合理设置超时时间避免长时间等待
5. **错误处理**：注意检查返回的result字段判断处理状态

## 版本与演进
- v1：初始版本，仅内存返回结果；支持单设备、多命令；模板为简单正则/字面匹配
- v1.1：引入 TextFSM 引擎（状态机、Required、Filldown、List、IgnoreCase）并保留回退路径；当无 `Record` 时为开发/调试友好自动产出记录
- 未来计划：
  - 支持多设备（保持轻耦合）
  - 增强 FSM 模板语义（字段映射、分组命名）
  - 增加可选的 MinIO 写入（按需开启）