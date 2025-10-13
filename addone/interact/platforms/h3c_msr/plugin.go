package h3c_msr

import "github.com/sshcollectorpro/sshcollectorpro/addone/interact"

// Plugin 为 h3c_msr 平台交互插件（H3C MSR 路由器）
type Plugin struct{}

func (p *Plugin) Name() string { return "h3c_msr" }

func (p *Plugin) Defaults() interact.InteractDefaults {
    // MSR 路由器命令较重，进一步提高超时
    return interact.InteractDefaults{
        Timeout:    75,
        Retries:    2,
        Threads:    4,
        Concurrent: 5,
        PromptSuffixes:   []string{">"},
        CommandIntervalMS: 200,
        AutoInteractions: []interact.AutoInteraction{
            {ExpectOutput: "more", AutoSend: " "},
            {ExpectOutput: "press any key", AutoSend: " "},
            {ExpectOutput: "confirm", AutoSend: "y"},
        },
        ErrorHints: []string{"error", "unrecognized command", "incomplete"},
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 关闭分页，避免长命令输出被暂停
    out := make([]string, 0, len(in.Commands)+1)
    out = append(out, "screen-length disable")
    out = append(out, in.Commands...)
    return interact.CommandTransformOutput{Commands: out}
}

func init() { interact.Register("h3c_msr", &Plugin{}) }