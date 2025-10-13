package huawei_s

import (
    "strings"
    "github.com/sshcollectorpro/sshcollectorpro/addone/collect"
)

// 仅处理 display version 回显
func parseDisplayVersionRow(ctx collect.ParseContext, raw string) collect.FormattedRow {
    lines := strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n")
    verLines := 0
    promptLines := 0
    for _, ln := range lines {
        low := strings.ToLower(ln)
        if strings.Contains(low, "version") { verLines++ }
        if strings.Contains(low, ">") || strings.Contains(low, "]") || strings.HasSuffix(strings.TrimSpace(ln), "#") { promptLines++ }
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