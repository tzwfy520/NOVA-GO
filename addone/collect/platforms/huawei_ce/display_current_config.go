package huawei_ce

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

type Plugin struct{}

func (p *Plugin) Name() string { return "huawei_ce" }

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
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    verLines := 0
    for _, ln := range lines {
        if strings.Contains(strings.ToLower(ln), "version") {
            verLines++
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
        },
    }
}

func init() {
    collect.Register("huawei_ce", &Plugin{})
}