package service

import "time"

// BackupBatchRequest 批量备份请求
type BackupBatchRequest struct {
    TaskID         string         `json:"task_id"`
    TaskName       string         `json:"task_name,omitempty"`
    TaskBatch      int            `json:"task_batch,omitempty"`
    SaveDir        string         `json:"save_dir,omitempty"`
    StorageBackend string         `json:"storage_backend,omitempty"` // local | minio（默认读取配置）
    RetryFlag      *int           `json:"retry_flag,omitempty"`
    Timeout        *int           `json:"timeout,omitempty"`
    Devices        []BackupDevice `json:"devices"`
}

// BackupDevice 备份的设备信息与命令
type BackupDevice struct {
    DeviceIP        string   `json:"device_ip"`
    Port            int      `json:"port,omitempty"`
    DeviceName      string   `json:"device_name,omitempty"`
    DevicePlatform  string   `json:"device_platform,omitempty"`
    CollectProtocol string   `json:"collect_protocol,omitempty"` // ssh
    UserName        string   `json:"user_name"`
    Password        string   `json:"password"`
    EnablePassword  string   `json:"enable_password,omitempty"`
    CliList         []string `json:"cli_list"`
}

// StoredObject 存储的对象信息
type StoredObject struct {
    URI         string `json:"uri"`
    Size        int64  `json:"size"`
    Checksum    string `json:"checksum"`
    ContentType string `json:"content_type"`
}

// CommandBackupResult 命令备份结果
type CommandBackupResult struct {
    Command        string         `json:"command"`
    RawOutput      string         `json:"raw_output"`
    RawOutputLines []string       `json:"raw_output_lines"`
    StoredObjects  []StoredObject `json:"stored_objects"`
    ExitCode       int            `json:"exit_code"`
    DurationMS     int64          `json:"duration_ms"`
    Error          string         `json:"error"`
}

// DeviceBackupResponse 设备备份响应
type DeviceBackupResponse struct {
    DeviceIP       string               `json:"device_ip"`
    Port           int                  `json:"port"`
    DeviceName     string               `json:"device_name,omitempty"`
    DevicePlatform string               `json:"device_platform,omitempty"`
    TaskID         string               `json:"task_id"`
    TaskBatch      int                  `json:"task_batch,omitempty"`
    Success        bool                 `json:"success"`
    Results        []CommandBackupResult `json:"results"`
    Error          string               `json:"error"`
    DurationMS     int64                `json:"duration_ms"`
    Timestamp      time.Time            `json:"timestamp"`
}

// BackupBatchResponse 批量备份响应
type BackupBatchResponse struct {
    Code    string                 `json:"code"`
    Message string                 `json:"message"`
    Data    []DeviceBackupResponse `json:"data"`
    Total   int                    `json:"total"`
}