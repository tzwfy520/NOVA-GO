package huawei_s

import "github.com/sshcollectorpro/sshcollectorpro/addone/interact"

// Plugin 为 huawei_s 平台交互插件（S 系列交换机）
type Plugin struct{}

func (p *Plugin) Name() string { return "huawei_s" }

func (p *Plugin) Defaults() interact.InteractDefaults {
    // 华为 S 系列通常命令响应较快，保持默认略微提高超时
    return interact.InteractDefaults{
        Timeout:    45,
        Retries:    2,
        Threads:    4,
        Concurrent: 5,
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 华为平台通常无需进入特权模式，直接返回
    return interact.CommandTransformOutput{Commands: append([]string{}, in.Commands...)}
}

func init() {
    interact.Register("huawei_s", &Plugin{})
}