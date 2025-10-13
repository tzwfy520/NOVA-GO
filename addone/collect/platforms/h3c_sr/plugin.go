package h3c_sr

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// Plugin 为 h3c_sr 平台采集插件（H3C SR 路由器）
type Plugin struct{}

func (p *Plugin) Name() string { return "h3c_sr" }

func (p *Plugin) StorageDefaults() collect.StorageDefaults { return (&collect.DefaultPlugin{}).StorageDefaults() }

// SystemCommands 返回系统内置的 H3C SR 采集命令
func (p *Plugin) SystemCommands() []string {
    return []string{
        "display current-configuration",
        "display version",
    }
}

// Parse 路由到具体命令处理
func (p *Plugin) Parse(ctx collect.ParseContext, raw string) (collect.ParseOutput, error) {
    cmd := strings.ToLower(strings.TrimSpace(ctx.Command))
    switch {
    case cmd == "display current-configuration" || strings.Contains(cmd, "display current") || strings.Contains(cmd, "display current-config"):
        row := parseDisplayCurrentRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil
    case cmd == "display version" || strings.Contains(cmd, "display ver"):
        row := parseDisplayVersionRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil
    default:
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: nil}, nil
    }
}

func init() { collect.Register("h3c_sr", &Plugin{}) }