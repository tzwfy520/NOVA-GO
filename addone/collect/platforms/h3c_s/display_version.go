package h3c_s

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

func parseDisplayVersionRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    hasVersion := false
    for _, ln := range lines {
        tl := strings.ToLower(strings.TrimSpace(ln))
        if strings.Contains(tl, "version") || strings.Contains(tl, "software") {
            hasVersion = true
            break
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
            "type":        "version",
            "line_count":  len(lines),
            "has_version": hasVersion,
        },
    }
}