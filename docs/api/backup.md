# 配置备份接口 API 文档

## 接口概览

配置备份服务提供网络设备配置的批量备份功能，支持将设备配置命令的执行结果直接存储到本地文件系统或 MinIO 对象存储中。

### API 端点

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/backup/batch` | 批量配置备份 |

## 批量配置备份

### 接口描述

执行批量设备配置备份，对指定设备列表执行配置命令并将结果存储到指定的存储后端。

### 请求参数

**HTTP 方法**: `POST`  
**路径**: `/api/v1/backup/batch`  
**Content-Type**: `application/json`

#### 请求体结构

```json
{
  "task_id": "string",
  "task_name": "string",
  "task_batch": "integer",
  "save_dir": "string",
  "storage_backend": "string",
  "retry_flag": "integer",
  "task_timeout": "integer",
  "devices": [
    {
      "device_ip": "string",
      "device_port": "integer",
      "device_name": "string",
      "device_platform": "string",
      "collect_protocol": "string",
      "user_name": "string",
      "password": "string",
      "enable_password": "string",
      "cli_list": ["string"],
      "device_timeout": "integer"
    }
  ]
}
```

#### 参数说明

**任务级参数**

| 参数名 | 类型 | 必填 | 默认值 | 描述 |
|--------|------|------|--------|------|
| `task_id` | string | 是 | - | 任务唯一标识符，用于追踪和存储路径生成 |
| `task_name` | string | 否 | - | 任务名称，用于标识和日志记录 |
| `task_batch` | integer | 否 | 0 | 任务批次号，用于同一任务的分批执行 |
| `save_dir` | string | 否 | - | 保存目录，与配置的前缀拼接形成最终存储路径 |
| `storage_backend` | string | 否 | 配置默认值 | 存储后端类型：`local`（本地文件）或 `minio`（对象存储） |
| `retry_flag` | integer | 否 | 0 | 重试次数，命令执行失败时的重试次数 |
| `task_timeout` | integer | 否 | 30 | 任务超时时间（秒），单个设备的总执行时间限制 |

**设备级参数**

| 参数名 | 类型 | 必填 | 默认值 | 描述 |
|--------|------|------|--------|------|
| `device_ip` | string | 是 | - | 设备 IP 地址 |
| `device_port` | integer | 否 | 22 | SSH 连接端口 |
| `device_name` | string | 否 | device_ip | 设备名称，用于存储路径和标识 |
| `device_platform` | string | 否 | default | 设备平台类型，影响交互方式和默认配置 |
| `collect_protocol` | string | 否 | ssh | 采集协议，目前仅支持 SSH |
| `user_name` | string | 是 | - | SSH 登录用户名 |
| `password` | string | 是 | - | SSH 登录密码 |
| `enable_password` | string | 否 | - | 特权模式密码（如 Cisco enable 密码） |
| `cli_list` | array[string] | 是 | - | 要执行的命令列表 |
| `device_timeout` | integer | 否 | task_timeout | 设备级超时时间（秒），覆盖任务级超时 |

#### 支持的设备平台

| 平台标识 | 描述 | 特性 |
|----------|------|------|
| `cisco_ios` | Cisco IOS 设备 | 支持 enable 模式，自动处理特权提升 |
| `cisco_nxos` | Cisco NX-OS 设备 | 支持 sudo 提权 |
| `cisco_iosxr` | Cisco IOS XR 设备 | 支持 admin 模式 |
| `huawei_vrp` | 华为 VRP 设备 | 支持 super 模式 |
| `h3c_comware` | H3C Comware 设备 | 支持 super 模式 |
| `juniper_junos` | Juniper JunOS 设备 | 支持 shell 和 cli 模式切换 |
| `arista_eos` | Arista EOS 设备 | 支持 enable 模式 |
| `linux` | Linux 服务器 | 支持 sudo 提权 |
| `default` | 通用设备 | 基础 SSH 交互 |

### 响应格式

#### 成功响应

**HTTP 状态码**: `200 OK`

```json
{
  "code": "SUCCESS",
  "message": "batch backup executed",
  "data": [
    {
      "device_ip": "192.168.1.1",
      "port": 22,
      "device_name": "switch-01",
      "device_platform": "cisco_ios",
      "task_id": "backup-001",
      "task_batch": 1,
      "success": true,
      "results": [
        {
          "command": "show running-config",
          "raw_output": "Building configuration...\n...",
          "raw_output_lines": ["Building configuration...", "..."],
          "stored_objects": [
            {
              "uri": "file:///data/backups/configs/switch-01/20241016_143022/backup-001/show-running-config.txt",
              "size": 15420,
              "checksum": "sha256:a1b2c3d4e5f6...",
              "content_type": "text/plain; charset=utf-8"
            }
          ],
          "exit_code": 0,
          "duration_ms": 1250,
          "error": ""
        }
      ],
      "error": "",
      "duration_ms": 3500,
      "timestamp": "2024-10-16T14:30:22.123Z"
    }
  ],
  "total": 1
}
```

#### 响应字段说明

**顶层响应结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `code` | string | 响应状态码：`SUCCESS`（全部成功）、`PARTIAL_SUCCESS`（部分成功）、`ERROR`（全部失败） |
| `message` | string | 响应消息描述 |
| `data` | array | 设备备份结果列表 |
| `total` | integer | 设备总数 |

**设备响应结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `device_ip` | string | 设备 IP 地址 |
| `port` | integer | SSH 连接端口 |
| `device_name` | string | 设备名称 |
| `device_platform` | string | 设备平台类型 |
| `task_id` | string | 任务 ID |
| `task_batch` | integer | 任务批次号 |
| `success` | boolean | 设备备份是否成功 |
| `results` | array | 命令执行结果列表 |
| `error` | string | 设备级错误信息（如连接失败） |
| `duration_ms` | integer | 设备总执行时间（毫秒） |
| `timestamp` | string | 执行时间戳（ISO 8601 格式） |

**命令结果结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `command` | string | 执行的命令 |
| `raw_output` | string | 命令原始输出 |
| `raw_output_lines` | array[string] | 按行分割的输出 |
| `stored_objects` | array | 存储对象信息列表 |
| `exit_code` | integer | 命令退出码 |
| `duration_ms` | integer | 命令执行时间（毫秒） |
| `error` | string | 命令级错误信息 |

**存储对象结构**

| 字段名 | 类型 | 描述 |
|--------|------|------|
| `uri` | string | 存储对象的 URI 路径 |
| `size` | integer | 文件大小（字节） |
| `checksum` | string | SHA256 校验和 |
| `content_type` | string | 内容类型 |

### 错误响应

#### 客户端错误（4xx）

**HTTP 状态码**: `400 Bad Request`

```json
{
  "code": "INVALID_REQUEST",
  "message": "task_id and devices are required"
}
```

**HTTP 状态码**: `400 Bad Request`

```json
{
  "code": "INVALID_PARAMS",
  "message": "task_id and devices are required"
}
```

#### 服务器错误（5xx）

**HTTP 状态码**: `500 Internal Server Error`

```json
{
  "code": "ERROR",
  "message": "backup service is not running"
}
```

#### 常见错误码

| 错误码 | 描述 | 解决方案 |
|--------|------|----------|
| `INVALID_REQUEST` | 请求格式错误 | 检查 JSON 格式和必填字段 |
| `INVALID_PARAMS` | 参数验证失败 | 确保 task_id 和 devices 不为空 |
| `ERROR` | 服务内部错误 | 检查服务状态和日志 |
| `PARTIAL_SUCCESS` | 部分设备失败 | 检查失败设备的具体错误信息 |

### 存储路径规则

#### 本地存储

存储路径格式：`{base_dir}/{prefix}/{save_dir}/{device_name}/{date_time}/{task_id}/{command_slug}.txt`

- `base_dir`: 配置的基础目录（默认 `./data/backups`）
- `prefix`: 配置的存储前缀
- `save_dir`: 请求中指定的保存目录
- `device_name`: 设备名称（缺失时使用 device_ip）
- `date_time`: 格式为 `YYYYMMDD_HHMMSS` 的时间戳
- `task_id`: 任务 ID
- `command_slug`: 命令的文件名形式（特殊字符转换为下划线）

示例：`./data/backups/system/configs/switch-01/20241016_143022/backup-001/show-running-config.txt`

#### MinIO 存储

对象键格式：`{prefix}/{save_dir}/{device_name}/{date_time}/{task_id}/{command_slug}.txt`

示例：`system/configs/switch-01/20241016_143022/backup-001/show-running-config.txt`

### 使用示例

#### 基础备份示例

```bash
curl -X POST "http://localhost:8080/api/v1/backup/batch" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "backup-20241016-001",
    "task_name": "daily-config-backup",
    "save_dir": "daily-backups",
    "storage_backend": "local",
    "devices": [
      {
        "device_ip": "192.168.1.1",
        "device_name": "core-switch-01",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "password123",
        "enable_password": "enable123",
        "cli_list": [
          "show running-config",
          "show startup-config",
          "show version"
        ]
      }
    ]
  }'
```

#### 批量多设备备份

```bash
curl -X POST "http://localhost:8080/api/v1/backup/batch" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "batch-backup-001",
    "task_name": "network-infrastructure-backup",
    "task_batch": 1,
    "save_dir": "infrastructure",
    "storage_backend": "minio",
    "retry_flag": 2,
    "task_timeout": 60,
    "devices": [
      {
        "device_ip": "192.168.1.1",
        "device_name": "core-switch-01",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "password123",
        "enable_password": "enable123",
        "cli_list": ["show running-config", "show version"]
      },
      {
        "device_ip": "192.168.1.2",
        "device_name": "access-switch-01",
        "device_platform": "cisco_ios",
        "user_name": "admin",
        "password": "password123",
        "cli_list": ["show running-config"]
      },
      {
        "device_ip": "192.168.1.10",
        "device_name": "router-01",
        "device_platform": "cisco_iosxr",
        "user_name": "admin",
        "password": "password123",
        "cli_list": ["show running-config", "show interfaces"]
      }
    ]
  }'
```

### 配置说明

#### 存储配置

备份服务的存储配置位于 `configs/config.yaml` 中：

```yaml
backup:
  storage_backend: "local"  # 默认存储后端：local 或 minio
  prefix: "backups"         # 存储路径前缀
  local:
    base_dir: "./data/backups"  # 本地存储基础目录
    mkdir_if_missing: true      # 自动创建目录
  aggregate:
    enabled: true               # 是否生成聚合文件
    filename: "all_commands.txt" # 聚合文件名

# MinIO 配置（复用全局存储配置）
storage:
  minio:
    host: "localhost"
    port: 9000
    access_key: "minioadmin"
    secret_key: "minioadmin"
    secure: false
    bucket: "ssh-collector"
```

#### 平台默认配置

不同设备平台的默认配置：

```yaml
collector:
  device_defaults:
    cisco_ios:
      timeout: 30
      retries: 1
      enable_mode: true
    huawei_vrp:
      timeout: 45
      retries: 2
      super_mode: true
    linux:
      timeout: 20
      retries: 1
      sudo_mode: true
```

### 最佳实践

#### 1. 任务 ID 命名规范

- 使用有意义的前缀：`backup-`, `config-`, `daily-`
- 包含时间戳：`backup-20241016-001`
- 避免特殊字符，使用连字符分隔

#### 2. 设备分组策略

- 按设备类型分组：相同平台的设备使用相同的超时和重试配置
- 按网络区域分组：不同网段的设备可能需要不同的超时设置
- 按重要性分组：核心设备可以设置更高的重试次数

#### 3. 命令选择建议

**Cisco IOS/IOS-XE**:
```json
"cli_list": [
  "show running-config",
  "show startup-config", 
  "show version",
  "show inventory",
  "show interfaces status"
]
```

**华为 VRP**:
```json
"cli_list": [
  "display current-configuration",
  "display saved-configuration",
  "display version",
  "display device"
]
```

**Linux 服务器**:
```json
"cli_list": [
  "cat /etc/hostname",
  "ip addr show",
  "systemctl status",
  "df -h"
]
```

#### 4. 错误处理

- 设置合理的超时时间：网络设备通常需要 30-60 秒
- 启用重试机制：网络不稳定时可以设置 1-2 次重试
- 监控存储空间：定期清理旧的备份文件
- 检查权限：确保用户有足够权限执行所需命令

#### 5. 性能优化

- 合理设置并发数：避免对设备造成过大压力
- 使用设备名称：便于文件组织和查找
- 选择合适的存储后端：本地存储适合小规模，MinIO 适合大规模部署
- 定期清理：设置备份文件的保留策略

### 故障排除

#### 常见问题

1. **连接超时**
   - 检查网络连通性
   - 增加 `task_timeout` 或 `device_timeout`
   - 确认 SSH 端口是否正确

2. **认证失败**
   - 验证用户名和密码
   - 检查是否需要 `enable_password`
   - 确认设备平台类型是否正确

3. **命令执行失败**
   - 检查命令语法是否正确
   - 确认用户权限是否足够
   - 查看设备响应中的错误信息

4. **存储失败**
   - 检查本地目录权限
   - 验证 MinIO 连接配置
   - 确认存储空间是否充足

#### 日志查看

备份服务的日志包含详细的执行信息：

```bash
# 查看服务日志
tail -f logs/ssh-collector.log | grep -i backup

# 查看特定任务的日志
grep "task_id=backup-001" logs/ssh-collector.log
```

#### 调试模式

启用调试模式可以获得更详细的执行信息：

```yaml
log:
  level: "debug"
  format: "json"
```