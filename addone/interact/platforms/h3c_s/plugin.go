package h3c_s

import "github.com/sshcollectorpro/sshcollectorpro/addone/interact"

// Plugin 为 h3c_s 平台交互插件（H3C 交换机）
type Plugin struct{}

func (p *Plugin) Name() string { return "h3c_s" }

func (p *Plugin) Defaults() interact.InteractDefaults {
    return interact.InteractDefaults{
        Timeout:    45,
        Retries:    2,
        Threads:    4,
        Concurrent: 5,
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 关闭分页，避免长命令输出被暂停
    out := make([]string, 0, len(in.Commands)+1)
    out = append(out, "screen-length disable")
    out = append(out, in.Commands...)
    return interact.CommandTransformOutput{Commands: out}
}

func init() { interact.Register("h3c_s", &Plugin{}) }