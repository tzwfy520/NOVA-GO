# SSH 交互时间设计与配置分析

## 1. 概述

本文档梳理 SSH Collector Pro 项目当前与“SSH 设备交互”相关的时间控制（Timeout/节奏）机制，说明配置与请求参数的优先级、实际生效路径（对应代码位置）、存在的偏差与风险，并给出可落地的优化建议。

## 2. 当前超时体系梳理

系统中的时间控制主要分布在 `configs/config.yaml`，并在代码层（`internal/config`、`internal/service`、`pkg/ssh`）中落地为多层 Context Deadline、连接拨号/握手截止时间、以及交互会话内的“节奏/超时”参数。

### 2.1 配置层级与优先级

按“生效强度”从高到低可分为 3 类：请求级（每次调用传入）、系统级强制中断（timeout_all）、交互节奏（interact_timeout）。其中 **请求级参数并不会覆盖系统级强制中断**，这一点在代码中是硬约束。

1. **请求级（每次任务）**
   - 位置：HTTP API 入参 `task_timeout` / `timeout`、`device_timeout`（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/api/handler/collector.go#L33-L120)）
   - 生效：作为“任务执行窗口”和“登录窗口”进入服务层，并最终用于创建 `execCtx/loginCtx`（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L361-L705) 与 [interact_basic.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/interact_basic.go#L50-L110)）
   - 关键字段：
     - `task_timeout`：影响执行窗口（软限），同时也用于 worker 队列等待超时
     - `device_timeout`：影响登录窗口（拨号/握手/认证）软限，缺省回退到 `task_timeout`

2. **系统级强制中断（timeout_all）**
   - 位置：
     - 平台级：`collector.device_defaults.<platform>.timeout.timeout_all`
     - 全局：`ssh.timeout.timeout_all`（最终被解析为 `config.SSH.Timeout`）
   - 生效：服务层使用 `GetTimeoutAll(platform)` 创建外层 `taskCtx`，超时后统一返回 `system interrupt: by timeout_all setting (Xs)`（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L361-L589)）
   - 优先级：平台 timeout_all > 全局 ssh.timeout.timeout_all > 默认 60s（见 [config.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/config/config.go#L474-L501)）
   - 重要约束：即使请求传入更大的 `task_timeout`，也不能突破 timeout_all；最终有效窗口为 `min(timeout_all, task_timeout, 上游 ctx deadline)`（由 Context 嵌套自然实现）

3. **交互节奏参数（interact_timeout）**
   - 位置：
     - 全局：`ssh.timeout.interact_timeout.*`
     - 平台：`collector.device_defaults.<platform>.timeout.interact_timeout.*`（以及 `collector.device_defaults.default.timeout.interact_timeout` 作为通用兜底）
   - 生效路径：
     - 加载配置时由 `config.Load` 读取 `ssh.timeout.interact_timeout.*` 到 `config.SSH.Interact`（见 [config.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/config/config.go#L246-L259)）
     - 运行时由 `getPlatformDefaults()` 先应用全局 `config.SSH.Interact` 作为 baseline，再用平台/默认平台的 `timeout.interact_timeout.*` 按字段覆盖，最终映射到 `ssh.InteractiveOptions`（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L99-L268)、[interact_basic.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/interact_basic.go#L158-L245)）
     - 配置下发服务中，`DeployService.getPlatformInteract()` 采用相同的“全局 baseline + 平台覆盖”策略（见 [deploy.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/deploy.go#L425-L510)）
   - 优先级：平台 `collector.device_defaults.<platform>.timeout.interact_timeout.*`（仅覆盖显式配置字段） > 全局 `ssh.timeout.interact_timeout.*` > 固定默认值（如 interval 120ms、per-command 30s 等）

### 2.2 关键超时参数详解

为了与代码一致，这里按“作用域”拆成 4 组：系统强制中断、任务/登录软限、连接拨号/握手、交互会话内节奏。

#### 2.2.1 系统强制中断：timeout_all（硬限）

| 参数名 | 说明 | 默认值/典型值 | 作用范围 |
| :--- | :--- | :--- | :--- |
| `timeout_all` | 系统强制中断窗口。超时后任务被强制标记失败并返回统一错误。 | 60s | 覆盖采集全流程（队列后执行阶段） |

#### 2.2.2 请求级软限：task_timeout / device_timeout（软限）

| 参数名 | 说明 | 默认值/典型值 | 作用范围 |
| :--- | :--- | :--- | :--- |
| `task_timeout` | 任务执行窗口（软限）。用于创建 `execCtx`，控制命令执行总窗口；也用于 worker 队列等待超时。 | 默认 30s（无请求、无平台 timeout 时） | 交互执行（含回退非交互）与队列等待 |
| `device_timeout` | 登录窗口（软限）。用于创建 `loginCtx`，用于连接获取（拨号/握手/认证）；缺省回退到 `task_timeout`。 | 继承 task_timeout | 连接建立（GetConnection/Connect） |

#### 2.2.3 连接拨号/握手：connect_timeout（由 dial/auth 合并）

| 参数名 | 说明 | 默认值/典型值 | 作用范围 |
| :--- | :--- | :--- | :--- |
| `dial_timeout` + `auth_timeout` | 配置层拆分字段；加载配置时合并为 `config.SSH.ConnectTimeout`。当前实现未区分拨号与鉴权阶段。 | 2s + 5s | 作为 Dialer.Timeout，同时用于握手阶段 `conn.SetDeadline` 的兜底（当 ctx 无 deadline 时） |

实现细节：`pkg/ssh/client.go` 连接时会优先使用 `ctx.Deadline()` 给底层 TCP conn 设置截止时间；否则使用 `ConnectTimeout` 作为握手截止（见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L180-L248)）。

#### 2.2.4 交互会话内节奏：interact_timeout（影响 InteractiveOptions）

| 参数名 | 说明 | 典型值 | 作用逻辑 |
| :--- | :--- | :--- | :--- |
| `command_interval_ms` | 命令发送间隔。 | 120ms | 每条命令结束后 sleep，避免设备限流/分页。 |
| `command_timeout_sec` | 单命令超时。 | 30s | 单条命令最久等待时间；超时会标记该命令 `command timeout` 并继续进入下一条。 |
| `quiet_after_ms` | 静默完成阈值。 | 800ms | 命令已有输出内容且该时间内无新输出，则认为命令完成（提示符识别失败时的自愈）。 |
| `quiet_poll_interval_ms` | 静默轮询间隔。 | 250ms | 每隔该间隔评估一次静默完成条件。 |
| `prompt_inducer_interval_ms` | 初始提示符诱发间隔。 | 1000ms | 仅在交互 Shell 启动后、首次提示符检测前，周期性发送 CRLF 促使设备吐出提示符。 |
| `prompt_inducer_max_count` | 初始提示符诱发次数上限。 | 12-30 | 仅用于“首次提示符检测”阶段；同时首次提示符等待还有 10s 的上限（见实现）。 |
| `enable_password_fallback_ms` | enable 密码回退发送延迟。 | 1500ms | 执行提权命令时，若未命中密码提示，在该延迟后主动发送一次密码，减少卡死概率。 |
| `exit_pause_ms` | 退出命令间隔。 | 150ms | 交互结束后按 `exit/quit` 序列退出时的节奏控制。 |

实现细节：
- 初始提示符诱发器：见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L627-L844)
- 静默完成与无输出命令完成：见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L953-L1238)
- 单命令超时：见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L988-L1238)

### 2.3 执行模型与上下文传播

当前采集执行存在两层 Context（再叠加上游 HTTP ctx），它们的语义不同：

1. **系统强制中断上下文（外层 taskCtx）**
   - 创建位置：`CollectorService.ExecuteTask`
   - 超时来源：`GetTimeoutAll(platform)`（平台 timeout_all 或全局 ssh.timeout.timeout_all）
   - 目的：保证系统在最坏情况下可以回收任务资源，并统一返回“系统强制中断”错误（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L361-L589)）

2. **任务执行上下文（内层 execCtx）**
   - 创建位置：`InteractBasic.Execute`
   - 超时来源：请求 `TaskTimeoutSec`（默认 30s，或平台 defaults.Timeout，或 API 入参）
   - 目的：控制本次任务“命令执行窗口”（包括交互失败回退为非交互的窗口）（见 [interact_basic.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/interact_basic.go#L72-L110)）

3. **登录上下文（内层 loginCtx）**
   - 创建位置：`InteractBasic.Execute`
   - 超时来源：请求 `DeviceTimeoutSec`，若更短则生效；否则回退到 `execCtx` 或上游更紧的 ctx deadline
   - 目的：控制连接获取/连接建立窗口（见 [interact_basic.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/interact_basic.go#L80-L110)）

4. **连接层截止时间**
   - 创建位置：`pkg/ssh/client.go` 的 `Connect`
   - 行为：DialContext 由 `loginCtx` 控制；握手阶段通过 `conn.SetDeadline` 绑定到 `loginCtx.Deadline()`（优先）或 `ConnectTimeout`（兜底）（见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L180-L248)）

## 3. 合理性评估

### 3.1 优点
- 强制中断兜底：timeout_all 作为硬限能保证最终资源回收，避免“卡死任务”无限占用 worker 与连接池。
- 请求级弹性：task_timeout / device_timeout 让调用方可以在任务维度调整窗口，适配不同命令复杂度与网络质量。
- 交互自愈机制：静默完成、无输出命令完成、enable 密码回退等策略提升网络设备 CLI 的交互稳定性（见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L953-L1274)）。

### 3.2 潜在风险与不足

1. **timeout_all 与 task_timeout 语义容易混淆且存在“不可突破”约束**
   - timeout_all 是系统强制中断，task_timeout 是执行软限；当 `task_timeout > timeout_all` 时，最终仍会被 timeout_all 截断。
   - 这会让调用方误以为“把 task_timeout 调大就能跑更久”，但实际上仍可能在 timeout_all 到点后被强制中断。

2. **prompt_inducer_* 只影响“首次提示符检测”，原文档口径不准确**
   - 提示符诱发器仅在 Shell 启动后、首次提示符检测前周期性发送 CRLF，检测到首个提示符或 10s 超过后即停止（见 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L627-L844)）。
   - 因此 `max_count * interval` 并不等价于“每条命令可能额外消耗的时间”，其影响主要集中在会话启动阶段，且有 10s 软上限。

3. **dial_timeout/auth_timeout 平台级字段目前不参与运行时逻辑**
   - 平台 defaults 的 `dial_timeout/auth_timeout` 会被解析到结构体，但当前 `getPlatformDefaults()` 未使用它们；连接阶段只使用全局 `ConnectTimeout` 与 `loginCtx` deadline（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L99-L268) 与 [client.go](file:///Users/wangfuyu/PythonCode/SSH-GO/pkg/ssh/client.go#L180-L248)）。

4. **错误语义与日志仍需统一口径**
   - 连接阶段：`InteractBasic.Execute` 识别典型超时并返回 `设备登陆超时(%ds): ...`，同时在日志中记录 `error` 与 `timeout` 字段（见 [interact_basic.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/interact_basic.go#L90-L123)）。
   - 交互阶段：当交互窗口 `execCtx` 超时（`context.DeadlineExceeded`）时，保留部分结果，并返回 `设备交互超时(%ds): ...`（见 [interact_basic.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/interact_basic.go#L244-L266)）。
   - 系统强制中断：timeout_all 到点统一返回 `system interrupt: by timeout_all setting (Xs)`（见 [collector.go](file:///Users/wangfuyu/PythonCode/SSH-GO/internal/service/collector.go#L560-L609)）。

## 4. 优化建议

### 4.1 策略优化建议

1. **明确并固化两类窗口的产品语义**
   - 建议将 timeout_all 文案统一为“系统强制中断窗口”，task_timeout 文案统一为“本次任务执行窗口”，并在 API 文档中强调：task_timeout 不会突破 timeout_all。
   - 若需要让调用方可覆盖强制中断窗口，建议新增白名单/上限（例如：`min(request_timeout_all, platform_timeout_all_max)`），避免被外部请求无限放大。

2. **补齐 dial/auth “拆分超时”的真实实现或调整文档口径**
   - 当前实现仅支持 `dial_timeout + auth_timeout => ConnectTimeout`（合并窗口），若确实需要细分，可在 `Connect` 中分别对 Dial 与 `ssh.NewClientConn` 设置不同的 deadline。
   - 若不需要细分，建议在文档/配置描述中把 `dial_timeout/auth_timeout` 明确为“合并后的连接与握手窗口组成部分”，避免误解。

### 4.2 配置结构调整建议

建议在 `config.yaml` 中增加明确的注释，避免配置与实现错配：
- `timeout_all`：系统强制中断窗口（硬限，覆盖所有逻辑）
- `task_timeout/device_timeout`：请求级软限（不会突破 timeout_all）
- `dial_timeout/auth_timeout`：当前仅用于合并为 ConnectTimeout（连接拨号+握手兜底窗口）
- `interact_timeout`：全局 ssh.timeout.interact_timeout 作为 baseline，平台 `collector.device_defaults.*.timeout.interact_timeout` 按字段覆盖

```yaml
# 建议配置示例
collector:
  device_defaults:
    default:
      timeout:
        interact_timeout:
          command_interval_ms: 120
          command_timeout_sec: 30
          quiet_after_ms: 800
          quiet_poll_interval_ms: 250
          prompt_inducer_interval_ms: 1000
          prompt_inducer_max_count: 12
          exit_pause_ms: 150
          enable_password_fallback_ms: 1500
    cisco_ios:
      timeout:
        timeout_all: 90
        interact_timeout:
          prompt_inducer_max_count: 10
```
