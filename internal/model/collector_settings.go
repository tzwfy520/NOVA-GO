package model

import "time"

// CollectorSettings 用于保存快速采集的全局设置
// 仅一行记录（ID=1），包含重试次数与超时
// 表名：collector_settings
// 该设置供前端页面读取与保存，后端在未传参时可作为默认值

type CollectorSettings struct {
	ID        uint      `gorm:"primaryKey"`
	RetryFlag int       `gorm:"not null;default:0"` // 重试次数
	Timeout   int       `gorm:"not null;default:30"` // 任务超时（秒）
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (CollectorSettings) TableName() string {
	return "collector_settings"
}