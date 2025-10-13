package model

import (
	"time"
)

// Task 采集任务
type Task struct {
	ID          string    `json:"id" gorm:"primaryKey;type:varchar(64)"`
	CollectorID string    `json:"collector_id" gorm:"type:varchar(64);not null;index"`
	Type        string    `json:"type" gorm:"type:varchar(32);not null"`
	DeviceIP    string    `json:"device_ip" gorm:"type:varchar(64);not null"`
	DevicePort  int       `json:"device_port" gorm:"not null;default:22"`
	Username    string    `json:"username" gorm:"type:varchar(64);not null"`
	Password    string    `json:"password" gorm:"type:varchar(256);not null"`
	Commands    string    `json:"commands" gorm:"type:text;not null"`
	Status      string    `json:"status" gorm:"type:varchar(16);not null;default:'pending'"`
	Result      string    `json:"result" gorm:"type:text"`
	ErrorMsg    string    `json:"error_msg" gorm:"type:text"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Duration    int64     `json:"duration"` // 执行时长，毫秒
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 表名
func (Task) TableName() string {
	return "tasks"
}

// TaskStatus 任务状态枚举
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusSuccess   = "success"
	TaskStatusFailed    = "failed"
	TaskStatusTimeout   = "timeout"
	TaskStatusCancelled = "cancelled"
)

// TaskType 任务类型枚举
const (
    TaskTypeSimple = "simple"
)

// TaskLog 任务日志
type TaskLog struct {
	ID        string    `json:"id" gorm:"primaryKey;type:varchar(64)"`
	TaskID    string    `json:"task_id" gorm:"type:varchar(64);not null;index"`
	Level     string    `json:"level" gorm:"type:varchar(16);not null"`
	Message   string    `json:"message" gorm:"type:text;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

// TableName 表名
func (TaskLog) TableName() string {
	return "task_logs"
}

// DeviceInfo 设备信息
type DeviceInfo struct {
	ID         string    `json:"id" gorm:"primaryKey;type:varchar(64)"`
	IP         string    `json:"ip" gorm:"type:varchar(64);not null;uniqueIndex"`
	Port       int       `json:"port" gorm:"not null;default:22"`
	DeviceType string    `json:"device_type" gorm:"type:varchar(32)"`
	Vendor     string    `json:"vendor" gorm:"type:varchar(64)"`
	Model      string    `json:"model" gorm:"type:varchar(64)"`
	Version    string    `json:"version" gorm:"type:varchar(64)"`
	Username   string    `json:"username" gorm:"type:varchar(64)"`
	Password   string    `json:"password" gorm:"type:varchar(256)"`
	Status     string    `json:"status" gorm:"type:varchar(16);default:'unknown'"`
	LastCheck  time.Time `json:"last_check"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName 表名
func (DeviceInfo) TableName() string {
	return "device_info"
}