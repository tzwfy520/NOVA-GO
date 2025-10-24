package model

import "time"

// SimNamespace 模拟命名空间配置
// 表名：sim_namespaces
// name 作为唯一键标识命名空间（如 default）
type SimNamespace struct {
	ID          uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string    `json:"name" gorm:"type:varchar(64);uniqueIndex;not null"`
	Port        int       `json:"port"`
	IdleSeconds int       `json:"idle_seconds"`
	MaxConn     int       `json:"max_conn"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (SimNamespace) TableName() string { return "sim_namespaces" }

// SimDeviceType 模拟设备类型配置
// 表名：sim_device_types
// type 作为唯一键标识设备类型（如 cisco、huawei、linux）
type SimDeviceType struct {
	ID                 uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Type               string    `json:"type" gorm:"type:varchar(64);uniqueIndex;not null"`
	PromptSuffixe      string    `json:"prompt_suffixe"`
	EnableModeRequired bool      `json:"enable_mode_required"`
	EnableModeSuffixe  string    `json:"enable_mode_suffixe"`
	ConfigModeSuffixe  string    `json:"config_mode_suffixe"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt          time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (SimDeviceType) TableName() string { return "sim_device_types" }

// SimDeviceName 设备名称到设备类型的映射
// 表名：sim_device_names
// name 作为唯一键标识设备名称，device_type 指向 SimDeviceType.Type（不强制外键）
type SimDeviceName struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	Name       string    `json:"name" gorm:"type:varchar(128);uniqueIndex;not null"`
	DeviceType string    `json:"device_type" gorm:"type:varchar(64);not null"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (SimDeviceName) TableName() string { return "sim_device_names" }