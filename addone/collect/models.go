package collect

import (
    "encoding/json"
)

// TaskStatus 任务状态（与系统主任务状态对齐）
const (
    TaskStatusSuccess = "success"
    TaskStatusFailed  = "failed"
)

// BaseRecord 所有表必须包含的基础字段
type BaseRecord struct {
    TaskID       string `json:"task_id"`
    TaskStatus   string `json:"task_status"`
    RawStoreJSON string `json:"raw_store_path"` // 原始数据存储路径（JSON），key为命令，value为对象存储路径
}

// RawStorePaths 原始数据映射（命令 -> 对象路径）
type RawStorePaths map[string]string

func (r RawStorePaths) Marshal() string {
    if r == nil {
        return "{}"
    }
    b, _ := json.Marshal(r)
    return string(b)
}

// MatchType 拼接匹配类型
type MatchType string

const (
    MatchExact MatchType = "exact"  // 完成匹配（完全匹配）
    MatchRegex MatchType = "regex"  // 正则匹配
)

// FieldMatch 字段匹配规则
// 当 Type=exact 时，仅使用 Field 进行等值匹配
// 当 Type=regex 时，使用 ExistingRegex/UpdateRegex 进行双向正则匹配
type FieldMatch struct {
    Field        string    `json:"field"`
    Type         MatchType `json:"type"`
    ExistingRegex string   `json:"existing_regex,omitempty"`
    UpdateRegex   string   `json:"update_regex,omitempty"`
}

// MergeSpec 拼接规则定义
// 默认条件：task_id 必须一致（同一任务下进行拼接）
type MergeSpec struct {
    // 必选条件（默认包含 task_id 相等）
    Conditions map[string]interface{} `json:"conditions"`
    // 字段匹配规则（一个或多个）
    Matches    []FieldMatch           `json:"matches"`
}

// FormattedRow 格式化后的单行数据，包含目标表、字段和值，及可选拼接规则
type FormattedRow struct {
    Table string                 `json:"table"`
    Base  BaseRecord             `json:"base"`
    Data  map[string]interface{} `json:"data"`
    // 当需要对已有数据进行更新或合并时，提供拼接规则
    Merge *MergeSpec             `json:"merge,omitempty"`
}