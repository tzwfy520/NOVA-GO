package model

import (
	"time"
)

// Collector 采集器信息
type Collector struct {
	ID         string    `json:"id" gorm:"primaryKey;type:varchar(64)"`
	Type       string    `json:"type" gorm:"type:varchar(32);not null"`
	Version    string    `json:"version" gorm:"type:varchar(32);not null"`
	Tags       string    `json:"tags" gorm:"type:text"`
	ServerIP   string    `json:"server_ip" gorm:"type:varchar(64);not null"`
	ServerPort int       `json:"server_port" gorm:"not null"`
	Threads    int       `json:"threads" gorm:"not null;default:10"`
	Concurrent int       `json:"concurrent" gorm:"not null;default:5"`
	Status     string    `json:"status" gorm:"type:varchar(16);not null;default:'offline'"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 表名
func (Collector) TableName() string {
	return "collectors"
}

// CollectorStatus 采集器状态
type CollectorStatus struct {
	ID               string    `json:"id" gorm:"primaryKey;type:varchar(64)"`
	CPUUsage         float64   `json:"cpu_usage" gorm:"type:decimal(5,2)"`
	MemoryUsage      float64   `json:"memory_usage" gorm:"type:decimal(5,2)"`
	DiskUsage        float64   `json:"disk_usage" gorm:"type:decimal(5,2)"`
	TaskSuccessCount int64     `json:"task_success_count" gorm:"default:0"`
	TaskFailureCount int64     `json:"task_failure_count" gorm:"default:0"`
	LastHeartbeat    time.Time `json:"last_heartbeat"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 表名
func (CollectorStatus) TableName() string {
	return "collector_status"
}