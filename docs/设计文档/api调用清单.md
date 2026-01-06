# API 调用清单（采集 / 备份 / 格式化 / 下发）

本文档基于当前代码实现梳理项目对外提供的主要接口：采集（collector）、备份（backup）、格式化（formatted）、下发（deploy），用于联调与对接。

## 1. 通用约定

### 1.1 基础地址

- 默认监听地址：`0.0.0.0:18000`（见 [config.yaml](file:///Users/wangfuyu/PythonCode/SSH-GO/configs/config.yaml#L1-L20)）
- 示例 BaseURL：`http://127.0.0.1:18000`

### 1.2 请求头与 Content-Type

- `Content-Type: application/json`
- 可选：`X-Request-ID: <string>`（若不传，服务端会生成并在响应头回传，见 [router.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/router/router.go#L219-L230)）

### 1.3 通用错误响应

多数接口在参数错误或执行失败时返回：

```json
{
  "code": "INVALID_PARAMS",
  "message": "请求参数无效: <原因>"
}
```

### 1.4 超时与重试字段约定

- `task_timeout`：任务级超时（秒）。采集侧在路由校验中限制 `<= 300`（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L753-L789)）。
- `device_timeout`：设备级超时（秒）。同样限制 `<= 300`。
- `retry_flag`：重试次数。采集/备份/格式化均存在该字段（部分接口为可选指针类型）。

## 2. 采集接口（/api/v1/collector）

### 2.1 快速采集（Fast）

- **方法**：POST
- **路径**：`/api/v1/collector/fast`
- **用途**：单设备快速采集，不做任务持久化；直接返回采集结果（见 [FastCollect](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L74-L135)）

**请求体（FastCollectRequest）**（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L58-L72)）：

```json
{
  "device_ip": "192.168.1.10",
  "device_port": 22,
  "device_name": "Core-SW-01",
  "device_platform": "cisco_ios",
  "collect_protocol": "ssh",
  "retry_flag": 1,
  "timeout": 30,
  "task_timeout": 30,
  "user_name": "admin",
  "password": "******",
  "enable_password": "******",
  "cli_list": ["show version", "show running-config"],
  "device_timeout": 30
}
```

**调用示例（curl）**：

```bash
curl -sS -X POST "http://127.0.0.1:18000/api/v1/collector/fast" \
  -H "Content-Type: application/json" \
  -d '{
    "device_ip":"192.168.1.10",
    "device_port":22,
    "device_platform":"cisco_ios",
    "collect_protocol":"ssh",
    "user_name":"admin",
    "password":"******",
    "cli_list":["show version"]
  }'
```

**返回示例**（顶层为 `code/message/data`，`data` 结构来自 [CollectResponse](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L63-L74)）：

```json
{
  "code": "SUCCESS",
  "message": "快速采集完成",
  "data": {
    "task_id": "fast-1234567890",
    "success": true,
    "results": [
      {
        "command": "show version",
        "output": "...",
        "error": "",
        "elapsed": "120ms",
        "exit_code": 0
      }
    ],
    "error": "",
    "duration_ms": 120,
    "timestamp": "2026-01-06T00:00:00Z",
    "metadata": {
      "collect_mode": "fast"
    },
    "log_file_path": "logs/collection/<task_id>_<YYYYMMDD_HHMMSS>.log"
  }
}
```

### 2.2 批量采集（原始批量）

- **方法**：POST
- **路径**：`/api/v1/collector/batch`
- **用途**：直接传入 `[]CollectRequest`，服务端逐条同步执行并返回数组结果（见 [BatchExecute](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L259-L343)）

**请求体**：`[]CollectRequest`（见 [CollectRequest](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L43-L61)）

```json
[
  {
    "task_id": "batch-001-1",
    "task_name": "批量采集示例",
    "device_ip": "192.168.1.10",
    "device_port": 22,
    "device_platform": "cisco_ios",
    "collect_protocol": "ssh",
    "user_name": "admin",
    "password": "******",
    "cli_list": ["show version"],
    "retry_flag": 1,
    "task_timeout": 60,
    "device_timeout": 60,
    "metadata": {"collect_mode": "batch"}
  }
]
```

**返回示例**：

```json
{
  "code": "SUCCESS",
  "message": "批量任务执行完成",
  "data": [
    {
      "task_id": "batch-001-1",
      "success": true,
      "results": [],
      "error": "",
      "duration_ms": 120,
      "timestamp": "2026-01-06T00:00:00Z",
      "metadata": {"collect_mode": "batch"},
      "log_file_path": "logs/collection/<task_id>_<YYYYMMDD_HHMMSS>.log"
    }
  ],
  "total": 1
}
```

### 2.3 批量采集（自定义拆封：custom）

- **方法**：POST
- **路径**：`/api/v1/collector/batch/custom`
- **用途**：更贴近对接侧的一次性批量入口；每台设备内部拆分为 `task_id-序号` 的子任务并并发执行（见 [BatchExecuteCustomer](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L391-L563)）

**请求体（CustomerBatchRequest）**（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L345-L366)）：

```json
{
  "task_id": "custom_task_001",
  "task_name": "网络设备配置采集",
  "retry_flag": 1,
  "task_timeout": 300,
  "devices": [
    {
      "device_ip": "192.168.1.10",
      "device_port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "******",
      "enable_password": "******",
      "cli_list": ["show version", "show running-config"],
      "device_timeout": 60
    }
  ]
}
```

**调用示例（curl）**：

```bash
curl -sS -X POST "http://127.0.0.1:18000/api/v1/collector/batch/custom" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id":"custom_task_001",
    "devices":[
      {
        "device_ip":"192.168.1.10",
        "device_platform":"cisco_ios",
        "collect_protocol":"ssh",
        "user_name":"admin",
        "password":"******",
        "cli_list":["show version"]
      }
    ]
  }'
```

**返回示例**（按设备组织，顶层返回 `code/message/data/total/log_file_path`；单设备条目为 map 结构）：

```json
{
  "code": "SUCCESS",
  "message": "自定义批量任务执行完成",
  "data": [
    {
      "device_ip": "192.168.1.10",
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "task_id": "custom_task_001-1",
      "success": true,
      "results": [],
      "error": "",
      "duration_ms": 120,
      "timestamp": "2026-01-06T00:00:00Z"
    }
  ],
  "total": 1,
  "log_file_path": "logs/collection/custom_task_001_20260106_000135.log"
}
```

### 2.4 批量采集（系统预制：system）

- **方法**：POST
- **路径**：`/api/v1/collector/batch/system`
- **用途**：同样为拆封批量入口，但强制要求传入 `device_platform`（见 [BatchExecuteSystem](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L565-L751)）
- **注意**：当前实现“仅使用用户提供的命令列表”，不会注入平台默认命令（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L650-L674)）

**请求体（SystemBatchRequest）**（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L368-L389)）：

```json
{
  "task_id": "system_task_001",
  "task_name": "系统预制采集",
  "retry_flag": 1,
  "task_timeout": 300,
  "device_list": [
    {
      "device_ip": "192.168.1.10",
      "device_port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "******",
      "enable_password": "******",
      "cli_list": ["show version"],
      "device_timeout": 60
    }
  ]
}
```

**返回示例**（可能返回 `SUCCESS` 或 `PARTIAL_SUCCESS`，见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L717-L749)）：

```json
{
  "code": "SUCCESS",
  "message": "系统预制批量任务执行完成",
  "data": [
    {
      "device_ip": "192.168.1.10",
      "port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "task_id": "system_task_001-1",
      "success": true,
      "results": [],
      "error": "",
      "duration_ms": 120,
      "timestamp": "2026-01-06T00:00:00Z"
    }
  ],
  "total": 1,
  "log_file_path": "logs/collection/system_task_001_20260106_000135.log"
}
```

### 2.5 任务状态与取消

- **方法**：GET
- **路径**：`/api/v1/collector/task/{task_id}/status`（见 [GetTaskStatus](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L137-L174)）
- **方法**：POST
- **路径**：`/api/v1/collector/task/{task_id}/cancel`（见 [CancelTask](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L176-L212)）

**状态返回示例**（实际返回字段为 `task_id/status/start_time/duration`）：

```json
{
  "task_id": "custom_task_001-1",
  "status": "running",
  "start_time": "2026-01-06T00:00:00Z",
  "duration": "1.2s"
}
```

### 2.6 统计与快速采集设置

- GET `/api/v1/collector/stats`（见 [GetStats](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L214-L229)）
- GET `/api/v1/collector/settings`（读取 sqlite 的快速采集默认重试/超时，见 [GetCollectorSettings](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L804-L828)）
- POST `/api/v1/collector/settings`（写入 sqlite，见 [UpdateCollectorSettings](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L830-L864)）

## 3. 备份接口（/api/v1/backup）

### 3.1 批量备份

- **方法**：POST
- **路径**：`/api/v1/backup/batch`（见 [BatchBackup](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/backup.go#L16-L36)）
- **用途**：批量执行命令并将结果写入存储（local/minio），返回每台设备每条命令的存储对象信息

**请求体（BackupBatchRequest）**（见 [backup.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/backup.go#L30-L40)）：

```json
{
  "task_id": "backup_task_001",
  "task_name": "设备配置备份",
  "task_batch": 1,
  "save_dir": "daily",
  "storage_backend": "minio",
  "retry_flag": 1,
  "task_timeout": 300,
  "devices": [
    {
      "device_ip": "192.168.1.10",
      "device_port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "******",
      "enable_password": "******",
      "cli_list": ["show running-config"],
      "device_timeout": 60
    }
  ]
}
```

**调用示例（curl）**：

```bash
curl -sS -X POST "http://127.0.0.1:18000/api/v1/backup/batch" \
  -H "Content-Type: application/json" \
  -d '{
    "task_id":"backup_task_001",
    "save_dir":"daily",
    "devices":[
      {
        "device_ip":"192.168.1.10",
        "device_platform":"cisco_ios",
        "collect_protocol":"ssh",
        "user_name":"admin",
        "password":"******",
        "cli_list":["show running-config"]
      }
    ]
  }'
```

**返回示例（BackupBatchResponse）**（见 [backup.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/backup.go#L89-L96)）：

```json
{
  "code": "SUCCESS",
  "message": "OK",
  "data": [
    {
      "device_ip": "192.168.1.10",
      "port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "task_id": "backup_task_001-1",
      "task_batch": 1,
      "success": true,
      "results": [
        {
          "command": "show running-config",
          "raw_output": "...",
          "stored_objects": [
            {
              "uri": "minio://<bucket>/<prefix>/daily/backup_task_001/<device>/<cmd>.txt",
              "size": 1234,
              "checksum": "sha256:...",
              "content_type": "text/plain"
            }
          ],
          "exit_code": 0,
          "duration_ms": 120,
          "error": ""
        }
      ],
      "error": "",
      "duration_ms": 120,
      "timestamp": "2026-01-06T00:00:00Z"
    }
  ],
  "total": 1,
  "log_file_path": "logs/backup/<task_id>_<YYYYMMDD_HHMMSS>.log"
}
```

## 4. 格式化接口（/api/v1/formatted）

### 4.1 批量格式化（存储）

- **方法**：POST
- **路径**：`/api/v1/formatted/batch`（见 [BatchFormatted](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/formatted.go#L52-L73)）
- **用途**：对采集输出按 FSM 模板进行解析/聚合，并将结果写入存储（实现位于 [format.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/format.go#L24-L109)）

**请求体（FormatBatchRequest）**（见 [format.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/format.go#L26-L35)）：

```json
{
  "task_id": "format_task_001",
  "task_name": "格式化任务",
  "task_batch": 1,
  "retry_flag": 1,
  "save_dir": "daily",
  "task_timeout": 300,
  "fsm_templates": [
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        {"cli_name": "show version", "fsm_value": "Value HOSTNAME (\\S+)\\nStart\\n..."}
      ]
    }
  ],
  "devices": [
    {
      "device_ip": "192.168.1.10",
      "device_port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "******",
      "enable_password": "******",
      "cli_list": ["show version"],
      "device_timeout": 60
    }
  ]
}
```

**返回示例（FormatBatchResponse）**（见 [format.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/format.go#L89-L108)）：

```json
{
  "code": "SUCCESS",
  "message": "OK",
  "json_prefix": "data-formats/daily/format_task_001/formatted/",
  "date_time": "20260106_000135",
  "login_failures": [],
  "collect_failures": [],
  "failed_commands": [],
  "fsm_notfound": [],
  "stats": {
    "total_devices": 1,
    "fully_success_devices": 1,
    "login_failed_devices": 0,
    "collect_failed_devices": 0,
    "parse_failed_devices": 0
  },
  "stored_objects": [
    {
      "uri": "minio://<bucket>/data-formats/daily/format_task_001/formatted/Core-SW-01.json",
      "size": 1234,
      "checksum": "sha256:...",
      "content_type": "application/json"
    }
  ],
  "log_file_path": "./logs/collector.log"
}
```

### 4.2 快速格式化（不强制存储）

- **方法**：POST
- **路径**：`/api/v1/formatted/fast`（见 [FastFormatted](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/formatted.go#L75-L96)）
- **用途**：针对单设备快速执行采集 + 解析，直接返回格式化 JSON（见 [FormatFastRequest/Response](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/format.go#L113-L154)）

**请求体（FormatFastRequest）**：

```json
{
  "task_id": "format_fast_001",
  "task_name": "快速格式化",
  "retry_flag": 1,
  "task_timeout": 60,
  "device": [
    {
      "device_ip": "192.168.1.10",
      "device_port": 22,
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "******",
      "cli": "show version",
      "device_timeout": 60
    }
  ],
  "fsm_templates": [
    {
      "device_platform": "cisco_ios",
      "templates_values": [
        {"cli_name": "show version", "fsm_value": "Value HOSTNAME (\\S+)\\nStart\\n..."}
      ]
    }
  ]
}
```

**返回示例（FormatFastResponse）**（字段含义见 [format.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/format.go#L138-L154)）：

```json
{
  "code": "SUCCESS",
  "message": "OK",
  "task_id": "format_fast_001",
  "date_time": "20260106_000135",
  "result": "success",
  "device": {
    "device_ip": "192.168.1.10",
    "device_name": "Core-SW-01",
    "device_platform": "cisco_ios"
  },
  "raw": [],
  "formatted_json": {"hostname": "Core-SW-01"},
  "log_file_path": "./logs/collector.log"
}
```

## 5. 下发接口（/api/v1/deploy）

### 5.1 快速下发

- **方法**：POST
- **路径**：`/api/v1/deploy/fast`（见 [FastDeploy](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/deploy.go#L19-L49)）
- **用途**：设备配置下发；可选下发前后状态采集（status_check_enable=1 且提供 status_check_list）

**请求体（DeployFastRequest）**（见 [deploy.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/deploy.go#L45-L54)）：

```json
{
  "task_id": "deploy_task_001",
  "task_name": "配置下发",
  "retry_flag": 0,
  "task_type": "exec",
  "task_timeout": 60,
  "status_check_enable": 1,
  "devices": [
    {
      "device_ip": "192.168.1.10",
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "device_port": 22,
      "collect_protocol": "ssh",
      "user_name": "admin",
      "password": "******",
      "enable_password": "******",
      "cli_list": ["show run | inc hostname"],
      "status_check_list": ["show run | inc hostname"],
      "config_deploy": "hostname Core-SW-01",
      "device_timeout": 60
    }
  ]
}
```

**返回示例（DeployFastResponse）**（见 [deploy.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/deploy.go#L72-L91)）：

```json
{
  "task_id": "deploy_task_001",
  "task_name": "配置下发",
  "results": [
    {
      "device_ip": "192.168.1.10",
      "device_name": "Core-SW-01",
      "device_platform": "cisco_ios",
      "device_status_before": {"show run | inc hostname": "hostname old"},
      "device_status_after": {"show run | inc hostname": "hostname Core-SW-01"},
      "deploy_log_exec": [
        {"command": "conf t ...", "output": "...", "error": "", "elapsed": "120ms", "exit_code": 0}
      ],
      "deploy_logs_aggregated": [],
      "error": ""
    }
  ],
  "duration": "2.1s",
  "log_file_path": "./logs/collector.log"
}
```

## 6. 辅助接口（可选）

- GET `/api/v1/health`：健康检查（见 [Health](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L231-L257)）
- GET `/api/v1/logs/tail?limit=200&q=xxx&level=info`：读取全局日志末尾 N 行（见 [TailLogs](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/logs.go#L20-L77)）

