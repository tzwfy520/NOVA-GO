package model

import "time"

// SSHPlatform 表示一个设备平台的SSH适配配置
// - ssh_type: 平台类型（如 default、cisco_ios、huawei 等）
// - vendor/system/remark: 可选元信息，作为注释写入YAML
// - params: 适配参数的JSON文本（复杂嵌套结构以JSON存储）
// - UpdatedAt: 最近更新时间，用于页面展示
//
// 说明：params 字段建议为JSON对象结构，键集合参考用户提供的示例：
//  prompt_suffixes、disable_paging_cmds、config_mode_clis、config_exit_cli、enable_required、
//  enable_cli、enable_except_output、skip_delayed_echo、timeout（含子字段）、
//  output_filter（含 prefixes/contains/case_insensitive/trim_space）、
//  interact（含 auto_interactions[{except_output,command_auto_send}], error_hints[], case_insensitive, trim_space）
//
// 注意：default 类型不允许删除（在删除接口中进行约束）。
//       ssh_type 不允许变更（作为唯一键）。

type SSHPlatform struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Type      string    `gorm:"column:ssh_type;uniqueIndex;not null" json:"ssh_type"`
	Vendor    string    `json:"vendor"`
	System    string    `json:"system"`
	Remark    string    `json:"remark"`
	Params    string    `gorm:"type:text" json:"params"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SSHPlatform) TableName() string { return "ssh_platforms" }