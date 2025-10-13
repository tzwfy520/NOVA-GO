package huawei_s

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// Plugin 为 huawei_s 平台插件
type Plugin struct{}

func (p *Plugin) Name() string { return "huawei_s" }

func (p *Plugin) StorageDefaults() collect.StorageDefaults { return (&collect.DefaultPlugin{}).StorageDefaults() }

// SystemCommands 返回系统内置的华为 S 系列采集命令
func (p *Plugin) SystemCommands() []string {
    return []string{
        "display current-configuration",
        "display version",
    }
}

// Parse 路由到具体命令处理
func (p *Plugin) Parse(ctx collect.ParseContext, raw string) (collect.ParseOutput, error) {
    cmd := strings.ToLower(strings.TrimSpace(ctx.Command))
    switch cmd {
    case "display current-configuration", "display current", "display current-config":
        row := parseDisplayCurrentRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil
    case "display version":
        row := parseDisplayVersionRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil
    default:
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: nil}, nil
    }
}

func init() { collect.Register("huawei_s", &Plugin{}) }