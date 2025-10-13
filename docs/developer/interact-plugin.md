# 设备交互插件规范与实现指南

本文档定义交互插件的规范、平台默认设置需求以及实现示例，帮助为不同设备平台提供一致、可扩展的交互行为。

## 总览
- 交互插件用于在采集执行前后对命令进行预处理，并提供平台默认运行参数。
- 当前接口位于 `addone/interact`：
  - `InteractPlugin`：插件接口
  - `InteractDefaults`：默认参数结构
  - `TransformCommands`：命令转换入口
  - `registry.go`：插件注册与获取

## 插件接口与默认参数

插件接口（参考 `addone/interact/default.go`）：
- `Name() string`：插件名称（如 `cisco_ios`、`huawei_s` 等）
- `Defaults() InteractDefaults`：返回平台默认运行参数
- `TransformCommands(in CommandTransformInput) CommandTransformOutput`：根据平台特性转换命令序列（如取消分页、进入特权）

已支持的默认参数字段：
- `Timeout int`：超时时间（秒）
- `Retries int`：重试次数
- `Threads int`：线程数（本地并行处理）
- `Concurrent int`：并发数（远端会话并发）
- `PromptSuffixes []string`：设备登陆后预期的提示符后缀（如 `#`、`>`、`]`），用于交互会话的提示检测。
- `CommandIntervalMS int`：多命令串行执行的间隔（毫秒）。
- `AutoInteractions []AutoInteraction`：自动交互对，当设备出现预期提示时自动下发命令。
- `ErrorHints []string`：命令错误提示关键词列表（供上层处理/记录）。

自动交互结构（已实现）：
```go
type AutoInteraction struct {
    ExpectOutput string // 预期出现的回显（子串匹配，大小写不敏感）
    AutoSend     string // 自动下发的命令
}
```

注：当前代码已在各平台的 `TransformCommands` 中实现“取消分页”的默认第一条命令（例如 Cisco 的 `terminal length 0`、华为/H3C 的 `screen-length disable`）。以上字段由服务层与 SSH 交互层读取并生效。

## 平台默认设置要求

每个平台插件需定义以下默认设置（通过 `Defaults()` 与 `TransformCommands()` 提供）：
- 取消输出限制命令：作为采集交互时的默认第一条命令。
- 预期符号（提示符后缀）：用于判断登录成功与命令结束（建议放入 `PromptSuffixes`）。
- 命令执行间隔：多个命令串行执行时的间隔（毫秒）。
- 自动执行交互参数对：当出现交互提示时，自动下发的命令对。
  - 示例：
    ```json
    [
      { "except_output": "do you want to save this config? yes/no", "command_auto_send": "yes" },
      { "except_output": "do you want to reload this device? yes/no", "command_auto_send": "no" }
    ]
    ```
- 命令错误提示：字符串列表，当设备出现以列表中的字符串开头的回显时，提示命令错误。
  - 示例：
    ```json
    [
      "ERROR:",
      "invalid parameters detect"
    ]
    ```

## 信息采集执行逻辑（与服务层协作）

- 多命令采集时，只登陆一次设备，使用同一交互式会话执行所有命令（已实现）。
- 采集结束后，确保会话正确关闭并归还连接到连接池（已实现）。
- 提示符匹配按插件 `PromptSuffixes` 生效；平台退出命令由服务层设置（Cisco：`exit`；Huawei/H3C：`quit`/`exit`）。
- 命令间隔按插件 `CommandIntervalMS` 生效；自动交互按插件 `AutoInteractions` 生效（如 `more`/`confirm`）。

## 实现示例

### Cisco IOS
文件：`addone/interact/platforms/cisco_ios/plugin.go`
- 取消分页：`terminal length 0`
- 可选特权：`enable`（由 `metadata["enable"]` 控制）
- 建议默认参数：
  - `Timeout=60`、`Retries=2`、`CommandIntervalMS=200`、`PromptSuffixes=["#"]`
  - `ErrorHints=["% Invalid", "ERROR:"]`
  - `AutoInteractions`：按需补充保存/重载提示的自动应答

### Huawei S/CE
文件：`addone/interact/platforms/huawei_s/plugin.go`、`addone/interact/platforms/huawei_ce/plugin.go`
- 取消分页：`screen-length disable`
- 建议默认参数：
  - `Timeout=45~60`、`Retries=2`、`CommandIntervalMS=150`、`PromptSuffixes=[">"]`
  - `ErrorHints=["Error:", "Unrecognized command"]`
  - `AutoInteractions`：如系统提示确认保存/重启，追加自动命令

### H3C S/SR/MSR
文件：`addone/interact/platforms/h3c_s/plugin.go`、`addone/interact/platforms/h3c_sr/plugin.go`、`addone/interact/platforms/h3c_msr/plugin.go`
- 取消分页：`screen-length disable`
- 建议默认参数：
  - `Timeout=45~75`、`Retries=2`、`CommandIntervalMS=150`、`PromptSuffixes=[">"]`
  - `ErrorHints=["Error:", "Unrecognized command"]`

## 注册与获取

- 在平台插件文件中调用：`interact.Register("<platform>", &Plugin{})`
- 服务层按 `device_platform` 获取插件：`interact.Get(platform)`，未注册平台返回 `default`。

## 测试建议
- 单平台命令转换测试：验证取消分页为第一条命令，命令序列正确追加。
- 超时与重试：模拟长命令与失败重试，确认默认参数被读取与生效。
- 自动交互：模拟设备回显触发 `AutoInteractions`，验证自动应答效果（已支持，基于子串匹配）。
- 错误提示：准备包含 `ErrorHints` 的回显，确认日志中记录并可用于后续处理（目前用于记录，后续可用于结果标注）。

## 后续优化建议

1. 将提示符逻辑（目前在服务层硬编码）迁移到插件默认参数（`PromptSuffixes`）。
2. 在 SSH 交互层添加命令级间隔控制（读取 `CommandIntervalMS`）。
3. 引入自动交互状态机，支持 `AutoInteractions`（包含 `once` 语义与正则匹配）。
4. 错误提示前缀统一处理：集中在会话层识别并返回结构化错误。
5. 为插件定义退出命令策略（如 `exit`/`quit`），确保优雅收尾。
6. 增强度量与日志（命令耗时、失败原因、自动交互触发次数等），便于优化。
7. 将取消分页命令可配置（少数设备可能使用不同命令或需要先进入特权）。
8. 添加单元测试与模拟设备回显框架，提高回归质量。

> 以上建议为规划方向，本次不改代码；落地需按接口适配服务层与 SSH 交互实现。