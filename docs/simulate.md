# 模拟设备 SSH 服务

本功能提供一个内置的 SSH 模拟服务端，用于在采集/解析开发时进行联调与回显验证。根据 `simulate/simulate.yaml` 定义的命名空间(namespace)、设备类型与设备名称，启动多个本地端口的 SSH 服务，登录后按设备名称与命令匹配返回对应的文本回显。

## 开启方式
- 在全局配置 `configs/config.yaml` 中设置：
  - `server.simulate_enable: true`
- 在项目根目录准备模拟配置：`simulate/simulate.yaml`（已提供示例）。
- 启动主服务后，模拟服务将根据 `simulate.yaml` 自动启动并监听对应端口。

## simulate.yaml 示例
```
namespace:
  default:
    port: 22001
    idle_seconds: 180
    max_conn: 5
  test-user:
    port: 22002
    idle_seconds: 180
    max_conn: 5

device_type:
  cisco_ios:
    prompt_suffixe: ">"
    enable_mode_required: true
    enable_mode_suffixe: "#"
  huawei:
    prompt_suffixe: ">"
    enable_mode_required: false

device_name:
  simulte-dev-cisco-01:
    device_type: cisco_ios
  simulte-dev-huawei-01:
    device_type: huawei
```

说明：
- `namespace.*.port`：每个命名空间对应一个 SSH 监听端口。
- `idle_seconds`：会话空闲超时，超过后自动断开。
- `max_conn`：并发连接上限，超过后新连接将被拒绝。
- 设备类型字段名采用 `prompt_suffixe`、`enable_mode_suffixe`（与需求一致）。
- `device_name`：设备名称清单；SSH 登录时使用“设备名称”作为“用户名”以匹配设备类型。

## 目录结构与自动生成
- 根目录：`simulate/namespace/`
- 启动时自动创建：`simulate/namespace/<namespace>/<device_name>/`
- 命令回显文件：在上述设备目录下放置以“命令名称”命名的 `.txt` 文件：
  - 例如：`simulate/namespace/default/simulte-dev-cisco-01/show running-config.txt`
  - 也支持下划线替代空格：`show_running-config.txt`

## 登录与回显规则
- 登录方式：用户名使用设备名称；登录密码为 `nova`。
  - 例：`ssh -p 22001 simulte-dev-cisco-01@127.0.0.1`（提示密码时输入 `nova`）
- 登录后根据设备类型返回提示符：`<device_name><prompt_suffixe>`，如：`simulte-dev-cisco-01>`。
- 执行命令：按当前命名空间与设备名称查找同名 `.txt` 文件，存在则返回文件内容；否则返回“未找到模拟命令”的提示。
- 提权(enable)：当 `enable_mode_required: true` 且输入 `enable` 时，提示 `Password:`，提权密码为 `nova`；校验通过后提示符切换为 `enable_mode_suffixe`（如 `#`）。
- 退出：输入 `exit` 或 `quit`。

## 设计与解耦
- 模拟服务代码位于 `simulate/Simulate.go`，与现有采集/备份/格式化服务解耦。
- 仅当 `server.simulate_enable` 为 `true` 且存在 `simulate/simulate.yaml` 时启动，不影响原有 HTTP/API 与业务逻辑。
- 日志输出沿用项目的 `pkg/logger`。

## 注意事项
- 该模拟服务用于功能联调与解析验证，不包含严格的安全认证与权限控制，请勿对外网开放。
- 当命令包含特殊字符时，最佳做法是使用“原命令命名的 `.txt` 文件”；同时提供以下回退匹配：空格替换为下划线的文件名。
- 若需扩展更复杂的交互（分页、模式切换等），可以在对应设备目录下编排更多命令文件，或在 `Simulate.go` 中补充自定义逻辑。