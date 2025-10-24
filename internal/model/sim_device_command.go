package model

import "time"

// SimDeviceCommand 针对命名空间与设备的模拟命令
// 表名：sim_device_commands
// - Namespace: 命名空间名称（指向 SimNamespace.Name，不强制外键）
// - DeviceName: 设备名称（指向 SimDeviceName.Name，不强制外键）
// - Command / Output: 模拟命令与回显
// - Enabled: 是否启用
// 说明：为避免外键导致的迁移复杂度，此处只使用字符串关联

type SimDeviceCommand struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Namespace  string    `json:"namespace" gorm:"type:varchar(64);index;not null"`
	DeviceName string    `json:"device_name" gorm:"type:varchar(128);index;not null"`
	Command    string    `json:"command" gorm:"type:text;not null"`
	Output     string    `json:"output" gorm:"type:text;not null"`
	Enabled    bool      `json:"enabled" gorm:"default:true"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (SimDeviceCommand) TableName() string { return "sim_device_commands" }