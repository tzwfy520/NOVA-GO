package huawei_s

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

type Plugin struct{}

func (p *Plugin) Name() string { return "huawei_s" }

func (p *Plugin) StorageDefaults() collect.StorageDefaults { return (&collect.DefaultPlugin{}).StorageDefaults() }

func (p *Plugin) Parse(ctx collect.ParseContext, raw string) (collect.ParseOutput, error) {
    switch strings.ToLower(strings.TrimSpace(ctx.Command)) {
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

func parseDisplayCurrentRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    // 存根：统计配置行数
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    return collect.FormattedRow{
        Table: "device_config",
        Base: collect.BaseRecord{
            TaskID:       ctx.TaskID,
            TaskStatus:   ctx.Status,
            RawStoreJSON: ctx.RawPaths.Marshal(),
        },
        Data: map[string]interface{}{
            "type":       "config",
            "line_count": len(lines),
        },
    }
}

func parseDisplayVersionRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    // 存根：提取包含 "Version" 与设备提示符的行数
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    verLines := 0
    promptLines := 0
    for _, ln := range lines {
        low := strings.ToLower(ln)
        if strings.Contains(low, "version") {
            verLines++
        }
        if strings.Contains(low, ">") || strings.Contains(low, "]") || strings.HasSuffix(strings.TrimSpace(ln), "#") {
            promptLines++
        }
    }
    return collect.FormattedRow{
        Table: "version_info",
        Base: collect.BaseRecord{
            TaskID:       ctx.TaskID,
            TaskStatus:   ctx.Status,
            RawStoreJSON: ctx.RawPaths.Marshal(),
        },
        Data: map[string]interface{}{
            "type":          "version",
            "version_lines": verLines,
            "prompt_lines":  promptLines,
        },
    }
}

func init() {
    collect.Register("huawei_s", &Plugin{})
}