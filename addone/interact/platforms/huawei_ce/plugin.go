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
    }
}

func (p *Plugin) TransformCommands(in interact.CommandTransformInput) interact.CommandTransformOutput {
    // 默认不做转换
    return interact.CommandTransformOutput{Commands: append([]string{}, in.Commands...)}
}

func init() {
    interact.Register("huawei_ce", &Plugin{})
}