package model

import "time"

// DeviceType 表示设备类型配置（用于页面“设备类型”管理）
// - vendor/system/kind: 必填，分别为厂商/操作系统/类型
// - tag: 选填，默认值为 default
// - ssh_type: 交互类型（从 SSHPlatform.ssh_type 下拉选择）
// - enabled: 是否启用，默认 true
// - 组合唯一键: vendor + system + kind + tag
//
// 注意：ID 为自增主键，仅用于后台标识，不在页面展示

type DeviceType struct {
    ID        uint      `gorm:"primaryKey" json:"id"`
    Vendor    string    `gorm:"not null" json:"vendor"`
    System    string    `gorm:"not null" json:"system"`
    Kind      string    `gorm:"not null" json:"kind"`
    Tag       string    `gorm:"not null;default:default" json:"tag"`
    SSHType   string    `gorm:"column:ssh_type;not null" json:"ssh_type"`
    Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

func (DeviceType) TableName() string { return "device_types" }