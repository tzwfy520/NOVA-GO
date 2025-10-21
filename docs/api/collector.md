# 采集接口 API 文档

本文档描述SSH采集器专业版的采集接口，包括批量采集、任务管理、状态查询等功能的输入/输出参数、字段含义、错误设计，以及系统支持的设备类型与内置格式化命令列表。

## 接口概览

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/collector/batch/custom` | 自定义批量采集 |
| GET | `/api/v1/collector/task/{task_id}/status` | 获取任务状态 |
| POST | `/api/v1/collector/task/{task_id}/cancel` | 取消任务 |
| GET | `/api/v1/collector/stats` | 获取采集统计信息 |
| GET | `/api/v1/health` | 健康检查 |

## 通用参数说明

### 任务级参数
- `task_id`：任务唯一标识，必填。用于任务追踪和状态查询。
- `task_name`：任务名称，选填。便于任务识别和管理。
- `retry_flag`：重试次数，选填。为空时使用系统内置交互默认值。
- `task_timeout`：任务超时时间（秒），选填。为空时使用系统内置交互默认值。

### 设备级参数
- `device_ip`：设备 IP 地址，必填。
- `device_name`：设备名称，选填。用于标识和日志记录。
- `device_platform`：设备平台，在系统批量接口中为必填。支持的平台包括：cisco、huawei、h3c、linux等。
- `collect_protocol`：采集协议，当前支持 `ssh`，选填。为空时默认按 SSH 处理。
- `device_port`：SSH 端口，选填。未提供或非法时默认 `22`。
- `user_name`：登录用户名，必填。
- `password`：登录密码，必填。
- `enable_password`：特权/enable 密码，选填。用于需要进入特权模式的设备（如 Cisco 的 `enable`）。
- `cli_list`：命令列表，可为空/一个/多个命令。
- `device_timeout`：设备级超时时间（秒），选填。覆盖任务级超时设置。

## 通用输出参数
- `task_id`：任务标识。
- `success`：任务整体是否成功（所有命令均成功）。
- `error`：错误信息（当存在错误时）。
- `timestamp`：服务端时间戳。
- `results`：采集结果列表，按设备组织。
- `device_info`：设备信息，包含IP、名称、平台等。
- `commands`：命令执行结果列表。
- `output`：命令输出内容。
- `success`：单个命令是否执行成功。
- `error`：命令执行错误信息（如有）。

## 自定义批量采集接口

### 接口描述
执行自定义批量设备采集任务，支持灵活的设备配置和命令组合。

### 请求参数
**HTTP 方法**: `POST`  
**路径**: `/api/v1/collector/batch/custom`  
**Content-Type**: `application/json`

#### 请求体结构
```json
{
  "task_id": "string",
  "task_name": "string",
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

#### 示例请求
```json
{
  "task_id": "custom_task_001",
  "task_name": "网络设备配置采集",
  "retry_flag": 3,
  "task_timeout": 300,
  "devices": [
    {
      "device_ip": "192.168.1.1",
      "device_port": 22,
      "device_name": "Core-Switch-01",
      "device_platform": "cisco",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "password123",
      "enable_password": "enable123",
      "cli_list": ["show version", "show running-config"],
      "device_timeout": 60
    }
  ]
}
```

### 响应格式
```json
{
  "task_id": "custom_task_001",
  "success": true,
  "timestamp": "2024-01-01T12:00:00Z",
  "results": [
    {
      "device_info": {
        "device_ip": "192.168.1.1",
        "device_name": "Core-Switch-01",
        "device_platform": "cisco",
        "port": 22
      },
      "success": true,
      "commands": [
        {
          "command": "show version",
          "output": "Cisco IOS Software...",
          "success": true,
          "duration": "2.5s"
        }
      ]
    }
  ]
}
```

## 系统批量采集接口

### 接口描述
执行系统预定义的批量设备采集任务，适用于标准化的采集场景。

### 请求参数
**HTTP 方法**: `POST`  
**路径**: `/api/v1/collector/batch/system`  
**Content-Type**: `application/json`

#### 请求体结构
```json
{
  "task_id": "string",
  "task_name": "string",
  "retry_flag": "integer",
  "task_timeout": "integer",
  "device_list": [
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

注意：系统批量接口中 `device_platform` 为必填字段。

## 任务状态查询接口

### 接口描述
根据任务ID获取任务的执行状态和进度信息。

### 请求参数
**HTTP 方法**: `GET`  
**路径**: `/api/v1/collector/task/{task_id}/status`

#### 路径参数
- `task_id`：任务ID，必填

### 响应格式
```json
{
  "task_id": "custom_task_001",
  "status": "running",
  "start_time": "2024-01-01T12:00:00Z",
  "duration": "30s"
}
```

#### 状态说明
- `pending`：任务等待中
- `running`：任务执行中
- `completed`：任务已完成
- `failed`：任务执行失败
- `cancelled`：任务已取消

## 任务取消接口

### 接口描述
取消正在执行的采集任务。

### 请求参数
**HTTP 方法**: `POST`  
**路径**: `/api/v1/collector/task/{task_id}/cancel`

#### 路径参数
- `task_id`：任务ID，必填

### 响应格式
```json
{
  "code": "SUCCESS",
  "message": "任务取消成功",
  "data": {
    "task_id": "custom_task_001"
  }
}
```

## 采集统计信息接口

### 接口描述
获取系统的采集统计信息，包括任务数量、成功率等。

### 请求参数
**HTTP 方法**: `GET`  
**路径**: `/api/v1/collector/stats`

### 响应格式
```json
{
  "total_tasks": 100,
  "running_tasks": 5,
  "completed_tasks": 90,
  "failed_tasks": 5,
  "success_rate": "90%",
  "active_connections": 15
}
```

## 健康检查接口

### 接口描述
检查采集服务的健康状态。

### 请求参数
**HTTP 方法**: `GET`  
**路径**: `/api/v1/health`

### 响应格式
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "version": "1.0.0",
  "uptime": "24h30m15s"
}
```

## 错误处理

### 错误响应格式
```json
{
  "code": "ERROR_CODE",
  "message": "错误描述信息"
}
```

### 常见错误码
- `MISSING_TASK_ID`：任务ID不能为空
- `TASK_NOT_FOUND`：任务不存在
- `INVALID_DEVICE_IP`：设备IP格式无效
- `CONNECTION_FAILED`：设备连接失败
- `AUTHENTICATION_FAILED`：认证失败
- `COMMAND_TIMEOUT`：命令执行超时
- `PLATFORM_NOT_SUPPORTED`：不支持的设备平台

### 故障排除指南
- **连接失败**：检查 `device_ip` 与 `device_port`、认证参数、以及网络连通性
- **认证失败**：验证 `user_name` 和 `password` 的正确性
- **提示符识别异常**：检查设备平台与提示符后缀。平台默认后缀示例：Cisco `#`/`>`，Huawei/H3C `]`
- **命令超时**：调整 `task_timeout` 或 `device_timeout` 参数
- **输出过滤**：默认移除分页提示，如需保留原始提示请调整配置

## 支持的设备平台

| 平台 | 标识符 | 默认提示符 | 特权模式 |
|------|--------|------------|----------|
| Cisco | cisco | `#` / `>` | 支持enable |
| Huawei | huawei | `]` | 支持system-view |
| H3C | h3c | `]` | 支持system-view |
| Linux | linux | `$` / `#` | 支持sudo |
| Juniper | juniper | `%` / `>` | 支持configure |

## 并发控制

系统支持并发控制以优化性能：
- **并发度**：通过 `collector.concurrent` 配置控制同时执行的任务数
- **连接池**：通过 `ssh.max_sessions` 控制SSH连接数上限
- **档位配置**：支持 S/M/L/XL 档位，自动映射并发参数

## 配置示例

### 高并发场景
```json
{
  "task_id": "high_concurrency_task",
  "task_name": "大规模设备采集",
  "retry_flag": 2,
  "task_timeout": 600,
  "devices": [
    // 大量设备配置...
  ]
}
```

### 关键设备场景
```json
{
  "task_id": "critical_device_task",
  "task_name": "核心设备采集",
  "retry_flag": 5,
  "task_timeout": 300,
  "devices": [
    {
      "device_ip": "192.168.1.1",
      "device_name": "Core-Router",
      "device_platform": "cisco",
      "user_name": "admin",
      "password": "secure_password",
      "enable_password": "enable_password",
      "cli_list": [
        "show version",
        "show running-config",
        "show interface status"
      ],
      "device_timeout": 120
    }
  ]
}
```