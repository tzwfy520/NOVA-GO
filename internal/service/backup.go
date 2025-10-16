package service

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "time"

    "github.com/sshcollectorpro/sshcollectorpro/internal/config"
    "github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
    "github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// BackupService 配置备份服务
type BackupService struct {
    config        *config.Config
    sshPool       *ssh.Pool
    running       bool
    workers       chan struct{}
    exec          *ExecAdapter
    storageWriter StorageWriter
}

// NewBackupService 创建备份服务
func NewBackupService(cfg *config.Config) *BackupService {
    poolConfig := &ssh.PoolConfig{
        MaxIdle:     10,
        MaxActive:   cfg.Collector.Concurrent,
        IdleTimeout: 5 * time.Minute,
        SSHConfig: &ssh.Config{
            Timeout:     cfg.SSH.Timeout,
            KeepAlive:   cfg.SSH.KeepAliveInterval,
            MaxSessions: cfg.SSH.MaxSessions,
        },
    }

    pool := ssh.NewPool(poolConfig)
    return &BackupService{
        config:        cfg,
        sshPool:       pool,
        workers:       make(chan struct{}, cfg.Collector.Concurrent),
        exec:          NewExecAdapter(pool, cfg),
        storageWriter: NewStorageWriter(cfg),
    }
}

// Start 启动服务
func (s *BackupService) Start(ctx context.Context) error {
    if s.running { return fmt.Errorf("backup service is already running") }
    s.running = true
    logger.Info("Backup service started")
    return nil
}

// Stop 停止服务
func (s *BackupService) Stop() error {
    if !s.running { return nil }
    s.running = false
    if err := s.sshPool.Close(); err != nil {
        logger.Error("Failed to close SSH pool (backup)", "error", err)
    }
    logger.Info("Backup service stopped")
    return nil
}

// ExecuteBatch 执行批量备份
func (s *BackupService) ExecuteBatch(ctx context.Context, req *BackupBatchRequest) (*BackupBatchResponse, error) {
    if !s.running { return nil, fmt.Errorf("backup service is not running") }
    if req == nil { return nil, fmt.Errorf("nil request") }
    if strings.TrimSpace(req.TaskID) == "" { return nil, fmt.Errorf("task_id is required") }
    if len(req.Devices) == 0 { return nil, fmt.Errorf("devices is empty") }

    // 并发执行各设备备份
    type item struct {
        resp DeviceBackupResponse
    }
    out := make([]item, len(req.Devices))
    var wg sync.WaitGroup
    wg.Add(len(req.Devices))

    for i := range req.Devices {
        idx := i
        dev := req.Devices[i]

        // 队列限流：等待工作令牌，避免 HTTP ctx 过早结束
        go func() {
            // 采用有效超时作为队列等待窗口
            effTimeout := s.effectiveTimeout(req.Timeout, dev.DevicePlatform)
            waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Duration(effTimeout)*time.Second)
            defer waitCancel()
            select {
            case s.workers <- struct{}{}:
                defer func() { <-s.workers }()
            case <-waitCtx.Done():
                out[idx].resp = DeviceBackupResponse{
                    DeviceIP:       dev.DeviceIP,
                    Port:           func() int { if dev.Port < 1 || dev.Port > 65535 { return 22 }; return dev.Port }(),
                    DeviceName:     dev.DeviceName,
                    DevicePlatform: dev.DevicePlatform,
                    TaskID:         req.TaskID,
                    TaskBatch:      req.TaskBatch,
                    Success:        false,
                    Error:          fmt.Sprintf("queue wait timeout after %ds", effTimeout),
                    DurationMS:     0,
                    Timestamp:      time.Now(),
                }
                wg.Done()
                return
            }

            start := time.Now()
            resp := DeviceBackupResponse{
                DeviceIP:       dev.DeviceIP,
                Port:           func() int { if dev.Port < 1 || dev.Port > 65535 { return 22 }; return dev.Port }(),
                DeviceName:     dev.DeviceName,
                DevicePlatform: dev.DevicePlatform,
                TaskID:         req.TaskID,
                TaskBatch:      req.TaskBatch,
                Timestamp:      start,
            }

            // 执行命令
            execReq := &ExecRequest{
                DeviceIP:        dev.DeviceIP,
                Port:            dev.Port,
                DeviceName:      dev.DeviceName,
                DevicePlatform:  dev.DevicePlatform,
                CollectProtocol: dev.CollectProtocol,
                UserName:        dev.UserName,
                Password:        dev.Password,
                EnablePassword:  dev.EnablePassword,
                TimeoutSec:      s.effectiveTimeout(req.Timeout, dev.DevicePlatform),
            }

            results, err := s.exec.Execute(ctx, execReq, dev.CliList)
            if err != nil {
                resp.Success = false
                resp.Error = err.Error()
                resp.DurationMS = time.Since(start).Milliseconds()
                out[idx].resp = resp
                wg.Done()
                return
            }

            // 写入存储并组装响应
            date := time.Now().Format("20060102")
            backend := strings.TrimSpace(req.StorageBackend)
            if backend == "" { backend = strings.TrimSpace(s.config.Backup.StorageBackend) }
            if backend == "" { backend = "local" }

            resp.Results = make([]CommandBackupResult, 0, len(results))
            for _, r := range results {
                // 预处理命令不落盘，仅记录输出（例如 enable、关闭分页等）
                isPre := s.isPreCommand(dev.DevicePlatform, r.Command)

                stored := []StoredObject{}
                storeErrMsg := ""
                // 当 aggregate_only 启用时，跳过逐命令写入，仅生成聚合文件
                if !isPre && !s.config.Backup.Aggregate.AggregateOnly {
                    // 仅对采集命令进行存储
                    meta := StorageMeta{
                        SaveDir:      req.SaveDir,
                        DateYYYYMMDD: date,
                        TimeHHMMSS:   start.Format("150405"),
                        TaskID:       req.TaskID,
                        DeviceName:   dev.DeviceName,
                        DeviceIP:     dev.DeviceIP,
                        CommandSlug:  r.Command,
                        Backend:      backend,
                    }
                    obj, werr := s.storageWriter.Write(ctx, meta, r.Output, "text/plain; charset=utf-8")
                    if obj.URI != "" {
                        stored = append(stored, obj)
                    }
                    if werr != nil {
                        storeErrMsg = werr.Error()
                    }
                }

                resp.Results = append(resp.Results, CommandBackupResult{
                    Command:        r.Command,
                    RawOutput:      r.Output,
                    RawOutputLines: func() []string { if r.Output == "" { return []string{} }; return strings.Split(r.Output, "\n") }(),
                    StoredObjects:  stored,
                    ExitCode:       r.ExitCode,
                    DurationMS:     r.Duration.Milliseconds(),
                    Error:          func() string { if r.Error != "" { return r.Error } ; return storeErrMsg }(),
                })
            }

            // 聚合写入：受配置控制，将所有采集命令输出汇总到单一文件（不包含预处理命令）
            // 当 aggregate_only=true 时，即便未显式开启 enabled，也生成聚合文件
            if s.config.Backup.Aggregate.Enabled || s.config.Backup.Aggregate.AggregateOnly {
                var aggBuilder strings.Builder
                // 统一的设备与时间，用于段落标识
                devName := strings.TrimSpace(dev.DeviceName)
                if devName == "" { devName = dev.DeviceIP }
                ts := start.Format("2006-01-02 15:04:05")
                for _, r := range resp.Results {
                    if s.isPreCommand(dev.DevicePlatform, r.Command) {
                        continue
                    }
                    cmdTitle := strings.TrimSpace(r.Command)
                    if cmdTitle == "" { cmdTitle = "unknown" }
                    // 段落头：=== <cmd> ===，下一行附设备名与时间
                    aggBuilder.WriteString("=== ")
                    aggBuilder.WriteString(cmdTitle)
                    aggBuilder.WriteString(" ===\n")
                    aggBuilder.WriteString("Device: ")
                    aggBuilder.WriteString(devName)
                    aggBuilder.WriteString(" | Time: ")
                    aggBuilder.WriteString(ts)
                    aggBuilder.WriteString("\n")
                    if r.RawOutput != "" {
                        aggBuilder.WriteString(r.RawOutput)
                        if !strings.HasSuffix(r.RawOutput, "\n") { aggBuilder.WriteString("\n") }
                    }
                    aggBuilder.WriteString("\n")
                }
                aggContent := aggBuilder.String()
                if strings.TrimSpace(aggContent) != "" {
                    // 聚合文件名可配置，允许带扩展名
                    aggName := strings.TrimSpace(s.config.Backup.Aggregate.Filename)
                    if aggName == "" { aggName = "all_cli.txt" }
                    metaAll := StorageMeta{
                        SaveDir:      req.SaveDir,
                        DateYYYYMMDD: date,
                        TimeHHMMSS:   start.Format("150405"),
                        TaskID:       req.TaskID,
                        DeviceName:   dev.DeviceName,
                        DeviceIP:     dev.DeviceIP,
                        CommandSlug:  aggName,
                        Backend:      backend,
                    }
                    obj, werr := s.storageWriter.Write(ctx, metaAll, aggContent, "text/plain; charset=utf-8")
                    storedList := []StoredObject{}
                    if obj.URI != "" { storedList = []StoredObject{obj} }
                    errMsg := ""
                    if werr != nil { errMsg = werr.Error() }
                    resp.Results = append(resp.Results, CommandBackupResult{
                        Command:        aggName,
                        RawOutput:      aggContent,
                        RawOutputLines: func() []string { return strings.Split(aggContent, "\n") }(),
                        StoredObjects:  storedList,
                        ExitCode:       0,
                        DurationMS:     0,
                        Error:          errMsg,
                    })
                }
            }

            // 成功条件：至少有结果且不含致命错误
            resp.Success = len(resp.Results) > 0 && resp.Error == ""
            resp.DurationMS = time.Since(start).Milliseconds()
            out[idx].resp = resp
            wg.Done()
        }()
    }

    wg.Wait()

    // 汇总响应
    final := &BackupBatchResponse{
        Code:    "SUCCESS",
        Message: "batch backup executed",
        Data:    make([]DeviceBackupResponse, 0, len(out)),
        Total:   len(out),
    }
    anyFail := false
    for _, it := range out {
        final.Data = append(final.Data, it.resp)
        if !it.resp.Success { anyFail = true }
    }
    if anyFail {
        final.Code = "PARTIAL_SUCCESS"
        final.Message = "some devices failed"
    }
    return final, nil
}

func (s *BackupService) effectiveTimeout(reqTimeout *int, platform string) int {
    if reqTimeout != nil && *reqTimeout > 0 { return *reqTimeout }
    d := getPlatformDefaults(strings.ToLower(strings.TrimSpace(func() string { if platform == "" { return "default" }; return platform }())))
    if d.Timeout > 0 { return d.Timeout }
    return 30
}

// isPreCommand 判断是否为平台级预处理命令（如 enable、关闭分页），这些命令不参与落盘
func (s *BackupService) isPreCommand(platform, cmd string) bool {
    c := strings.ToLower(strings.TrimSpace(cmd))
    if c == "" { return false }
    p := strings.ToLower(strings.TrimSpace(platform))

    dd, ok := s.config.Collector.DeviceDefaults[p]
    if !ok {
        if strings.HasPrefix(p, "huawei") { dd, ok = s.config.Collector.DeviceDefaults["huawei"] }
        if !ok && strings.HasPrefix(p, "h3c") { dd, ok = s.config.Collector.DeviceDefaults["h3c"] }
        if !ok && strings.HasPrefix(p, "cisco") { dd, ok = s.config.Collector.DeviceDefaults["cisco_ios"] }
        if !ok && strings.HasPrefix(p, "linux") { dd, ok = s.config.Collector.DeviceDefaults["linux"] }
    }
    if ok {
        // 提权命令
        ecmd := strings.TrimSpace(dd.EnableCLI)
        if ecmd == "" && dd.EnableRequired { ecmd = "enable" }
        if ecmd != "" && strings.ToLower(ecmd) == c { return true }
        // 关闭分页命令
        for _, pc := range dd.DisablePagingCmds {
            if strings.ToLower(strings.TrimSpace(pc)) == c { return true }
        }
    }
    // 通用兜底
    if c == "enable" || c == "terminal length 0" || c == "screen-length disable" { return true }
    return false
}