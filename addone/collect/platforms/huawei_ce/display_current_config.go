package huawei_ce

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

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