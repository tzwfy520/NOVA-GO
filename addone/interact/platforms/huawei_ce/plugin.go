package huawei_ce

import "github.com/sshcollectorpro/sshcollectorpro/addone/interact"

// Plugin 为 huawei_ce 平台交互插件（CE 系列数据中心交换机）
type Plugin struct{}

func (p *Plugin) Name() string { return "huawei_ce" }

func (p *Plugin) Defaults() interact.InteractDefaults {
    // 数据中心设备命令较重，适当增加超时
    return interact.InteractDefaults{
        Timeout:    60,
        Retries:    2,
        Threads:    4,
        Concurrent: 5,
        PromptSuffixes:   []string{">"},
        CommandIntervalMS: 180,
        AutoInteractions: []interact.AutoInteraction{
            {ExpectOutput: "more", AutoSend: " "},
            {ExpectOutput: "press any key", AutoSend: " "},
            {ExpectOutput: "confirm", AutoSend: "y"},
        },
        ErrorHints: []string{"error", "unrecognized command", "incomplete"},
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 关闭分页，确保长输出完整显示
    out := make([]string, 0, len(in.Commands)+1)
    out = append(out, "screen-length disable")
    out = append(out, in.Commands...)
    return interact.CommandTransformOutput{Commands: out}
}

func init() {
    interact.Register("huawei_ce", &Plugin{})
}