package model

import "time"

// SimCommand 模拟命令定义
// 用于在不连接真实设备时，根据平台返回预设输出
// 表名：sim_commands

type SimCommand struct {
	ID        uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Platform  string    `json:"platform" gorm:"type:varchar(64);index;not null"`
	Command   string    `json:"command" gorm:"type:text;not null"`
	Output    string    `json:"output" gorm:"type:text;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (SimCommand) TableName() string { return "sim_commands" }