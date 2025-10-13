package cisco_ios

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// Plugin 为 cisco_ios 平台采集插件
type Plugin struct{}

func (p *Plugin) Name() string { return "cisco_ios" }

func (p *Plugin) StorageDefaults() collect.StorageDefaults { return (&collect.DefaultPlugin{}).StorageDefaults() }

// SystemCommands 返回系统内置的 Cisco IOS 采集命令（具备格式化支持）
func (p *Plugin) SystemCommands() []string {
    return []string{
        "show run",
        "show version",
        "show interfaces",
    }
}

// Parse 按命令分发到对应的文件级处理函数
func (p *Plugin) Parse(ctx collect.ParseContext, raw string) (collect.ParseOutput, error) {
    cmd := strings.ToLower(strings.TrimSpace(ctx.Command))
    switch cmd {
    // show run 及常见等价命令
    case "show run", "show_running-config", "show_running_config":
        row := parseShowRunRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil

    // show version
    case "show version":
        row := parseShowVersionRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil

    // show interfaces
    case "show interfaces":
        rows := parseInterfacesRows(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: rows}, nil

    // show interface mac（示例：MAC 更新）
    case "show interface mac":
        rows := parseInterfaceMacUpdateRows(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: rows}, nil

    default:
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: nil}, nil
    }
}

func init() { collect.Register("cisco_ios", &Plugin{}) }