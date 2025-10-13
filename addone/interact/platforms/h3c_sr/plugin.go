package h3c_sr

import "github.com/sshcollectorpro/sshcollectorpro/addone/interact"

// Plugin 为 h3c_sr 平台交互插件（H3C SR 路由器）
type Plugin struct{}

func (p *Plugin) Name() string { return "h3c_sr" }

func (p *Plugin) Defaults() interact.InteractDefaults {
    // 路由器命令可能更重，适当提高超时
    return interact.InteractDefaults{
        Timeout:    60,
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

func init() { interact.Register("h3c_sr", &Plugin{}) }