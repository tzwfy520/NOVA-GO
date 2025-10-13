package cisco_ios

import "github.com/sshcollectorpro/sshcollectorpro/addone/interact"

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
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 如果需要启用特权模式，插入 enable 命令
    // 可通过 metadata["enable"] 控制，默认启用
    enable := true
    if v, ok := in.Metadata["enable"].(bool); ok {
        enable = v
    }
    if !enable {
        return interact.CommandTransformOutput{Commands: append([]string{}, in.Commands...)}
    }

    out := make([]string, 0, len(in.Commands)+1)
    out = append(out, "enable")
    out = append(out, in.Commands...)
    return interact.CommandTransformOutput{Commands: out}
}

func init() {
    // 注册到交互插件中心
    interact.Register("cisco_ios", &Plugin{})
}