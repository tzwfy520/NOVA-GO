package cisco_ios

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// Plugin 为 cisco_ios 平台采集插件（部分命令解析示例）
type Plugin struct{}

func (p *Plugin) Name() string { return "cisco_ios" }

func (p *Plugin) StorageDefaults() collect.StorageDefaults { return (&collect.DefaultPlugin{}).StorageDefaults() }

func (p *Plugin) Parse(ctx collect.ParseContext, raw string) (collect.ParseOutput, error) {
    // 根据命令路由解析
    cmd := strings.ToLower(strings.TrimSpace(ctx.Command))
    switch cmd {
    case "show run", "show_running-config", "show_running_config":
        row := parseShowRunRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil
    case "show version":
        row := parseShowVersionRow(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: []collect.FormattedRow{row}}, nil
    case "show interfaces":
        rows := parseInterfacesRows(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: rows}, nil
    case "show interface mac":
        rows := parseInterfaceMacUpdateRows(ctx, raw)
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: rows}, nil
    default:
        return collect.ParseOutput{Platform: ctx.Platform, Command: ctx.Command, Raw: raw, Rows: nil}, nil
    }
}

func parseShowRunRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    // 存根：返回行数与是否包含 hostname
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    hasHostname := false
    for _, ln := range lines {
        if strings.HasPrefix(strings.TrimSpace(strings.ToLower(ln)), "hostname") {
            hasHostname = true
            break
        }
    }
    return collect.FormattedRow{
        Table: "device_config",
        Base: collect.BaseRecord{
            TaskID:       ctx.TaskID,
            TaskStatus:   ctx.Status,
            RawStoreJSON: ctx.RawPaths.Marshal(),
        },
        Data: map[string]interface{}{
            "type":        "config",
            "line_count":  len(lines),
            "has_hostname": hasHostname,
        },
    }
}

func parseShowVersionRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    // 存根：提取包含 "Version" 的行数
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

// 接口信息（不带MAC），后续通过拼接更新 MAC 字段
func parseInterfacesRows(ctx collect.ParseContext, raw string) []collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    rows := make([]collect.FormattedRow, 0)
    for _, ln := range lines {
        ln = strings.TrimSpace(ln)
        if ln == "" {
            continue
        }
        parts := strings.Fields(ln)
        if len(parts) < 2 {
            continue
        }
        rows = append(rows, collect.FormattedRow{
            Table: "interfaces",
            Base: collect.BaseRecord{
                TaskID:       ctx.TaskID,
                TaskStatus:   ctx.Status,
                RawStoreJSON: ctx.RawPaths.Marshal(),
            },
            Data: map[string]interface{}{
                "int_name": parts[0],
                "int_ip":   parts[1],
                "int_mac":  "",
            },
        })
    }
    return rows
}

// 接口MAC更新，匹配并更新已有行（task_id 一致 + int_name 正则匹配）
func parseInterfaceMacUpdateRows(ctx collect.ParseContext, raw string) []collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    rows := make([]collect.FormattedRow, 0)
    for _, ln := range lines {
        ln = strings.TrimSpace(ln)
        if ln == "" {
            continue
        }
        parts := strings.Fields(ln)
        if len(parts) < 2 {
            continue
        }
        merge := &collect.MergeSpec{
            Conditions: map[string]interface{}{
                "task_id": ctx.TaskID,
            },
            Matches: []collect.FieldMatch{
                {
                    Field:        "int_name",
                    Type:         collect.MatchRegex,
                    ExistingRegex: `(?i)ethernet(\d+)/(\d+)`,
                    UpdateRegex:   `(?i)e(\d+)/(\d+)`,
                },
            },
        }
        rows = append(rows, collect.FormattedRow{
            Table: "interfaces",
            Base: collect.BaseRecord{
                TaskID:       ctx.TaskID,
                TaskStatus:   ctx.Status,
                RawStoreJSON: ctx.RawPaths.Marshal(),
            },
            Data: map[string]interface{}{
                "int_name": parts[0],
                "int_mac":  parts[1],
            },
            Merge: merge,
        })
    }
    return rows
}

func init() {
    collect.Register("cisco_ios", &Plugin{})
}