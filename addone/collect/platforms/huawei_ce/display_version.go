package huawei_ce

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// 仅处理 display version 回显
func parseDisplayVersionRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    verLines := 0
    for _, ln := range lines {
        if strings.Contains(strings.ToLower(ln), "version") { verLines++ }
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