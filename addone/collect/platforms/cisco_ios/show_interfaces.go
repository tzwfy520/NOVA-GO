package cisco_ios

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// 仅处理 show interfaces 回显（不带 MAC）
func parseInterfacesRows(ctx collect.ParseContext, raw string) []collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    rows := make([]collect.FormattedRow, 0)
    for _, ln := range lines {
        ln = strings.TrimSpace(ln)
        if ln == "" { continue }
        parts := strings.Fields(ln)
        if len(parts) < 2 { continue }
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