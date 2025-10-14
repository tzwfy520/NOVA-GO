package cisco_ios

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/interact"
)

// Plugin 为 cisco_ios 平台交互插件
type Plugin struct{}

func (p *Plugin) Name() string { return "cisco_ios" }

func (p *Plugin) Defaults() interact.InteractDefaults {
    // Cisco IOS 通常需要进入 enable 特权，设置更高的超时和并发
    return interact.InteractDefaults{
        Timeout:    60,
        Retries:    2,
        Threads:    4,
        Concurrent: 5,
        // 同时匹配用户 EXEC 模式 '>' 与特权模式 '#'
        PromptSuffixes:   []string{">", "#"},
        CommandIntervalMS: 200,
        AutoInteractions: []interact.AutoInteraction{
            {ExpectOutput: "--more--", AutoSend: " "},
            {ExpectOutput: "more", AutoSend: " "},
            {ExpectOutput: "press any key", AutoSend: " "},
            {ExpectOutput: "confirm", AutoSend: "y"},
            {ExpectOutput: "[yes/no]", AutoSend: "yes"},
        },
        ErrorHints: []string{
            "invalid input detected",
            "incomplete command",
            "ambiguous command",
            "unknown command",
        },
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 关闭分页确保完整输出：terminal length 0
    // 可通过 metadata["enable"] 控制是否先进入特权模式（默认启用）
    enable := true
    if v, ok := in.Metadata["enable"].(bool); ok {
        enable = v
    }

    pre := make([]string, 0, 2)
    if enable {
        pre = append(pre, "enable")
    }
    pre = append(pre, "terminal length 0")

    // 规范化用户命令，兼容常见缩写与误写
    // 将 show run / show runn / show running 等同规范为 show running-config
    normalized := make([]string, 0, len(in.Commands))
    for _, c := range in.Commands {
        trimmed := strings.TrimSpace(c)
        low := strings.ToLower(trimmed)
        switch low {
        case "show run", "show running", "show runn":
            normalized = append(normalized, "show running-config")
        default:
            // 宽松匹配误写：以 "show runn" 开头也归一化
            if strings.HasPrefix(low, "show runn") {
                normalized = append(normalized, "show running-config")
            } else {
                normalized = append(normalized, trimmed)
            }
        }
    }

    out := make([]string, 0, len(normalized)+len(pre))
    out = append(out, pre...)
    out = append(out, normalized...)
    return interact.CommandTransformOutput{Commands: out}
}

func init() {
    // 注册到交互插件中心
    interact.Register("cisco_ios", &Plugin{})
}