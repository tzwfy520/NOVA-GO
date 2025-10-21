# 配置下发接口 API 文档

## 接口概览

配置下发接口提供设备配置的快速下发与状态检查功能，支持批量设备操作、配置前后状态对比、错误检测和详细执行日志记录。适用于网络设备配置变更、批量部署和运维自动化场景。

### API 端点

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/deploy/fast` | 快速配置下发 |

## 快速配置下发

### 接口描述

对指定设备执行配置下发操作，支持配置前后状态检查、详细执行日志记录和错误检测。可以选择执行模式（实际执行或干运行）和状态检查功能。

### 请求参数

**HTTP 方法**: `POST`  
**路径**: `/api/v1/deploy/fast`  
**Content-Type**: `application/json`

#### 请求体结构

```json
{
  "task_id": "string",
  "task_name": "string",
  "retry_flag": "integer",
  "task_type": "string",
  "task_timeout": "integer",
  "status_check_enable": "integer",
  "devices": [
    {
      "device_ip": "string",
      "device_name": "string",
      "device_platform": "string",
      "device_port": "integer",
      "collect_protocol": "string",
      "user_name": "string",
      "password": "string",
      "enable_password": "string",
      "cli_list": ["string"],
      "status_check_list": ["string"],
      "config_deploy": "string",
      "device_timeout": "integer"
    }
  ]
}
```

#### 参数说明

**任务级参数**

| 参数名 | 类型 | 必填 | 默认值 | 描述 |
|--------|------|------|--------|------|
| `task_id` | string | 是 | - | 任务唯一标识符，用于追踪和日志记录 |
| `task_name` | string | 否 | - | 任务名称，用于标识和日志记录 |
| `retry_flag` | integer | 否 | 0 | 连接失败时的重试次数（总尝试次数=retry_flag+1） |
| `task_type` | string | 否 | exec | 执行类型：`exec`（实际执行）、`dry_run`（干运行） |
| `task_timeout` | integer | 否 | 15 | 任务超时时间（秒），单个设备的总执行时间限制 |
| `status_check_enable` | integer | 否 | 0 | 状态检查开关：`1`（开启）、`0`（关闭） |

**设备参数**

| 参数名 | 类型 | 必填 | 默认值 | 描述 |
|--------|------|------|--------|------|
| `devices` | array | 是 | - | 设备列表，支持批量操作 |
| `device_ip` | string | 是 | - | 设备 IP 地址 |
| `device_name` | string | 否 | device_ip | 设备名称，用于标识 |
| `device_platform` | string | 否 | default | 设备平台类型，影响交互方式和默认配置 |
| `device_port` | integer | 否 | 22 | SSH 连接端口 |
| `collect_protocol` | string | 否 | ssh | 连接协议，目前仅支持 SSH |
| `user_name` | string | 是 | - | SSH 登录用户名 |
| `password` | string | 是 | - | SSH 登录密码 |
| `enable_password` | string | 否 | - | 特权模式密码（如 Cisco enable 密码） |
| `cli_list` | array[string] | 否 | - | 配置命令列表，与 config_deploy 二选一 |
| `status_check_list` | array[string] | 否 | - | 状态检查命令列表，用于配置前后对比 |
| `config_deploy` | string | 否 | - | 配置内容（多行文本），与 cli_list 二选一 |
| `device_timeout` | integer | 否 | task_timeout | 设备级超时时间（秒），覆盖任务级超时 |

#### 支持的设备平台

| 平台标识 | 描述 | 特性 |
|----------|------|------|
| `cisco_ios` | Cisco IOS 设备 | 支持 enable 模式，自动进入配置模式 |
| `cisco_nxos` | Cisco NX-OS 设备 | 支持 sudo 提权，配置模式处理 |
| `cisco_iosxr` | Cisco IOS XR 设备 | 支持 admin 模式，配置提交 |
| `huawei_vrp` | 华为 VRP 设备 | 支持 super 模式，系统视图配置 |
| `h3c_comware` | H3C Comware 设备 | 支持 super 模式，系统视图配置 |
| `juniper_junos` | Juniper JunOS 设备 | 支持配置模式和提交操作 |
| `arista_eos` | Arista EOS 设备 | 支持 enable 模式，配置模式处理 |
| `linux` | Linux 服务器 | 支持 sudo 提权，脚本执行 |
| `default` | 通用设备 | 基础 SSH 交互 |

#### 执行类型说明

| 类型 | 描述 | 用途 |
|------|------|------|
| `exec` | 实际执行配置下发 | 正式的配置变更操作 |
| `dry_run` | 干运行模式 | 验证配置语法和连接性，不实际执行 |

### 响应格式

#### 成功响应

**HTTP 状态码**: `200 OK`

```json
{
  "task_id": "deploy-task-001",
  "task_name": "批量配置下发",
  "results": [
    {
      "device_ip": "192.168.1.1",
      "device_name": "core-switch-01",
      "device_platform": "cisco_ios",
      "device_status_before": {
        "show version": "Cisco IOS Software, Version 15.2...",
        "show interfaces brief": "Interface Status Protocol..."
      },
      "device_status_after": {
        "show version": "Cisco IOS Software, Version 15.2...",
        "show interfaces brief": "Interface Status Protocol..."
      },
      "deploy_log_exec": [
        {
          "command": "interface GigabitEthernet0/1",
          "output": "Switch(config)#interface GigabitEthernet0/1\nSwitch(config-if)#",
          "error": "",
          "elapsed": "0.5s",
          "exit_code": 0
        },
        {
          "command": "description Connected to Server",
          "output": "Switch(config-if)#description Connected to Server\nSwitch(config-if)#",
          "error": "",
          "elapsed": "0.3s",
          "exit_code": 0
        }
      ],
      "deploy_logs_aggregated": [
        {
          "command": "Configuration Summary",
          "output": "Successfully configured 2 commands",
          "error": "",
          "elapsed": "2.1s",
          "exit_code": 0
        }
      ]
    }
  ],
  "duration": "5.2s"
}
```

#### 响应字段说明

**顶层响应结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `task_id` | string | 任务 ID |
| `task_name` | string | 任务名称 |
| `results` | array | 设备执行结果列表 |
| `duration` | string | 总执行时间 |

**设备执行结果结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `device_ip` | string | 设备 IP 地址 |
| `device_name` | string | 设备名称 |
| `device_platform` | string | 设备平台类型 |
| `device_status_before` | object | 配置前状态检查结果（键值对：命令-输出） |
| `device_status_after` | object | 配置后状态检查结果（键值对：命令-输出） |
| `deploy_log_exec` | array | 详细执行日志，记录每条配置命令的执行情况 |
| `deploy_logs_aggregated` | array | 聚合执行日志，汇总信息 |
| `error` | string | 设备级错误信息（如有） |

**命令执行结果结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `command` | string | 执行的命令 |
| `output` | string | 命令输出 |
| `error` | string | 错误信息（如有） |
| `elapsed` | string | 命令执行耗时 |
| `exit_code` | integer | 命令退出码：0（成功）、-1（失败） |

### 错误响应

#### 客户端错误（4xx）

**HTTP 状态码**: `400 Bad Request`

```json
{
  "code": "BAD_REQUEST",
  "message": "请求参数无效: task_id is required"
}
```

#### 服务器错误（5xx）

**HTTP 状态码**: `500 Internal Server Error`

```json
{
  "code": "DEPLOY_FAILED",
  "message": "配置下发执行失败: SSH connection timeout"
}
```

#### 常见错误码

| 错误码 | 描述 | 解决方案 |
|--------|------|----------|
| `BAD_REQUEST` | 请求参数验证失败 | 检查必填字段和参数格式 |
| `DEPLOY_FAILED` | 配置下发执行失败 | 检查设备连接、认证信息和命令语法 |

## 使用示例

### 基础配置下发示例

```bash
curl -X POST http://localhost:8080/api/v1/deploy/fast \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "deploy-001",
    "task_name": "接口配置更新",
    "task_type": "exec",
    "task_timeout": 60,
    "status_check_enable": 1,
    "devices": [
      {
        "device_ip": "192.168.1.1",
        "device_name": "core-switch-01",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "password123",
        "enable_password": "enable123",
        "cli_list": [
          "interface GigabitEthernet0/1",
          "description Connected to Server",
          "no shutdown"
        ],
        "status_check_list": [
          "show interfaces GigabitEthernet0/1 status",
          "show running-config interface GigabitEthernet0/1"
        ]
      }
    ]
  }'
```

### 批量设备配置下发示例

```bash
curl -X POST http://localhost:8080/api/v1/deploy/fast \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "batch-deploy-001",
    "task_name": "批量VLAN配置",
    "task_type": "exec",
    "retry_flag": 1,
    "task_timeout": 120,
    "status_check_enable": 1,
    "devices": [
      {
        "device_ip": "192.168.1.10",
        "device_name": "access-switch-01",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "cisco123",
        "enable_password": "enable123",
        "config_deploy": "vlan 100\n name SERVERS\n exit\ninterface range GigabitEthernet0/1-10\n switchport mode access\n switchport access vlan 100",
        "status_check_list": [
          "show vlan brief",
          "show interfaces status"
        ]
      },
      {
        "device_ip": "192.168.1.11",
        "device_name": "access-switch-02",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "cisco123",
        "enable_password": "enable123",
        "config_deploy": "vlan 100\n name SERVERS\n exit\ninterface range GigabitEthernet0/1-10\n switchport mode access\n switchport access vlan 100",
        "status_check_list": [
          "show vlan brief",
          "show interfaces status"
        ]
      }
    ]
  }'
```

### 干运行模式示例

```bash
curl -X POST http://localhost:8080/api/v1/deploy/fast \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "dry-run-001",
    "task_name": "配置验证",
    "task_type": "dry_run",
    "task_timeout": 30,
    "status_check_enable": 0,
    "devices": [
      {
        "device_ip": "192.168.1.1",
        "device_name": "test-switch",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "password123",
        "enable_password": "enable123",
        "cli_list": [
          "show running-config",
          "show version"
        ]
      }
    ]
  }'
```

## 配置说明

### 服务配置

在 `config.yaml` 中配置配置下发服务：

```yaml
deploy:
  deploy_wait_ms: 2000  # 配置下发后等待时间（毫秒）

ssh:
  timeout: 30s          # SSH 连接超时时间
  connect_timeout: 10s  # SSH 连接建立超时
  keep_alive_interval: 30s  # 保活间隔
  max_sessions: 100     # 最大并发会话数
```

### 设备平台配置

配置不同设备平台的交互参数：

```yaml
collector:
  device_defaults:
    cisco_ios:
      enable_cli: "enable"
      enable_except_output: "Password:"
      prompt_suffixes: ["#", ">"]
      config_enter_cli: ["configure terminal"]
      config_exit_cli: "end"
      interact:
        error_hints: ["Invalid", "Incomplete", "Ambiguous"]
        command_interval_ms: 200
        command_timeout_sec: 30
    
    huawei_vrp:
      enable_cli: "super"
      enable_except_output: "Password:"
      prompt_suffixes: [">", "]"]
      config_enter_cli: ["system-view"]
      config_exit_cli: "quit"
      interact:
        error_hints: ["Error:", "Unrecognized", "Incomplete"]
        command_interval_ms: 150
        command_timeout_sec: 30
```

### 并发配置

配置下发服务支持设备级并发，可通过以下参数调整：

```yaml
ssh:
  max_sessions: 50      # 最大并发SSH会话数
  
deploy:
  deploy_wait_ms: 2000  # 配置执行后等待时间
```

## 最佳实践

### 任务规划

1. **任务分组**
   - 按设备类型或功能模块分组执行
   - 避免单次任务包含过多设备
   - 合理设置任务超时时间

2. **配置验证**
   - 使用 `dry_run` 模式验证配置语法
   - 在小范围设备上测试后再批量执行
   - 启用状态检查功能验证配置效果

3. **命令设计**
   - 使用具体的配置命令，避免交互式命令
   - 确保命令序列的逻辑正确性
   - 考虑命令执行的依赖关系

### 错误处理

1. **连接错误**
   - 设置合理的重试次数
   - 检查网络连通性和SSH服务状态
   - 验证认证信息的正确性

2. **配置错误**
   - 检查命令语法和设备支持情况
   - 验证特权级别是否足够
   - 关注错误提示信息进行问题定位

3. **超时处理**
   - 根据配置复杂度设置合理超时时间
   - 对于耗时操作适当增加设备级超时
   - 监控任务执行进度

### 性能优化

1. **并发控制**
   - 根据网络环境调整最大并发数
   - 避免对同一设备的并发操作
   - 合理分配SSH连接池资源

2. **批量优化**
   - 将相似配置的设备分组处理
   - 使用配置模板减少重复内容
   - 优化命令序列减少交互次数

### 故障排除

1. **日志分析**
   ```bash
   # 检查详细执行日志
   curl -X POST http://localhost:8080/api/v1/deploy/fast \
     -H "Content-Type: application/json" \
     -d '...' | jq '.results[0].deploy_log_exec'
   
   # 检查状态对比
   curl -X POST http://localhost:8080/api/v1/deploy/fast \
     -H "Content-Type: application/json" \
     -d '...' | jq '.results[0] | {before: .device_status_before, after: .device_status_after}'
   ```

2. **常见问题**
   - **配置未生效**: 检查是否正确进入配置模式
   - **权限不足**: 验证 enable_password 设置
   - **命令失败**: 查看 exit_code 和 error 字段
   - **超时问题**: 调整 device_timeout 参数

3. **调试技巧**
   - 使用 `dry_run` 模式测试连接和命令
   - 启用状态检查对比配置前后差异
   - 分析 `deploy_log_exec` 中的详细执行信息
   - 检查设备平台配置是否匹配实际设备

## 安全注意事项

1. **认证安全**
   - 使用强密码和定期更换
   - 避免在日志中记录敏感信息
   - 考虑使用密钥认证替代密码

2. **配置安全**
   - 验证配置内容的安全性
   - 避免执行可能影响设备稳定性的命令
   - 在生产环境前充分测试

3. **网络安全**
   - 确保SSH连接的安全性
   - 限制API访问权限
   - 监控异常操作和访问

## 版本说明

### 当前版本特性

- 支持批量设备配置下发
- 支持配置前后状态检查
- 支持多种设备平台
- 提供详细执行日志
- 支持干运行模式
- 自动错误检测和报告

### 未来计划

- 配置模板和变量替换
- 配置回滚功能
- 更多设备平台支持
- 配置合规性检查
- 可视化配置管理界面