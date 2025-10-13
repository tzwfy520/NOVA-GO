package h3c_s

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

func parseDisplayCurrentRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    hasHostname := false
    for _, ln := range lines {
        tl := strings.ToLower(strings.TrimSpace(ln))
        if strings.HasPrefix(tl, "hostname") || strings.Contains(tl, "sysname") {
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
            "type":         "config",
            "line_count":   len(lines),
            "has_hostname": hasHostname,
        },
    }
}