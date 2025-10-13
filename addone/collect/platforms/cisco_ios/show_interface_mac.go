package cisco_ios

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// 仅处理 show interface mac 回显（更新 MAC，带正则匹配）
func parseInterfaceMacUpdateRows(ctx collect.ParseContext, raw string) []collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    rows := make([]collect.FormattedRow, 0)
    for _, ln := range lines {
        ln = strings.TrimSpace(ln)
        if ln == "" { continue }
        parts := strings.Fields(ln)
        if len(parts) < 2 { continue }
        merge := &collect.MergeSpec{
            Conditions: map[string]interface{}{
                "task_id": ctx.TaskID,
            },
            Matches: []collect.FieldMatch{
                {
                    Field:         "int_name",
                    Type:          collect.MatchRegex,
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