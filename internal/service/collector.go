package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// CollectorService 采集器服务
type CollectorService struct {
    config  *config.Config
    sshPool *ssh.Pool
    interact *InteractBasic
    mutex   sync.RWMutex
    running bool
    tasks   map[string]*TaskContext
    workers chan struct{}
}

// TaskContext 任务上下文
type TaskContext struct {
	Task      *model.Task
	Cancel    context.CancelFunc
	StartTime time.Time
	Status    string
}

// CollectRequest 采集请求
type CollectRequest struct {
	TaskID          string                 `json:"task_id"`
	TaskName        string                 `json:"task_name,omitempty"`
	CollectOrigin   string                 `json:"collect_origin,omitempty"` // system | customer
	DeviceIP        string                 `json:"device_ip"`
	DeviceName      string                 `json:"device_name,omitempty"`
	DevicePlatform  string                 `json:"device_platform,omitempty"`
	CollectProtocol string                 `json:"collect_protocol,omitempty"` // ssh
	Port            int                    `json:"port,omitempty"`
	UserName        string                 `json:"user_name"`
	Password        string                 `json:"password"`
	EnablePassword  string                 `json:"enable_password,omitempty"`
	CliList         []string               `json:"cli_list"`
	RetryFlag       *int                   `json:"retry_flag,omitempty"`
	Timeout         *int                   `json:"timeout,omitempty"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// CollectResponse 采集响应
type CollectResponse struct {
	TaskID     string                 `json:"task_id"`
	Success    bool                   `json:"success"`
	Results    []*CommandResultView   `json:"results"`
	Error      string                 `json:"error"`
	Duration   time.Duration          `json:"duration"`
	DurationMS int64                  `json:"duration_ms"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// 内置交互默认值结构（替代原 addone/interact）
type platformInteractDefaults struct {
    Timeout           int
    Retries           int
    Threads           int
    Concurrent        int
    PromptSuffixes    []string
    CommandIntervalMS int
    AutoInteractions  []struct{ ExpectOutput, AutoSend string }
    ErrorHints        []string
    SkipDelayedEcho   bool
    // 交互匹配选项（平台 interact 配置）
    InteractCaseInsensitive bool
    InteractTrimSpace       bool
}

// getPlatformDefaults 仅从配置读取平台默认，若平台缺失则兜底使用 default
func getPlatformDefaults(platform string) platformInteractDefaults {
    p := strings.TrimSpace(strings.ToLower(platform))
    base := platformInteractDefaults{}
    if cfg := config.Get(); cfg != nil {
        if dd, ok := cfg.Collector.DeviceDefaults[p]; ok {
            if len(dd.PromptSuffixes) > 0 { base.PromptSuffixes = dd.PromptSuffixes }
            base.SkipDelayedEcho = dd.SkipDelayedEcho
            // 优先使用平台嵌套 interact，其次兼容旧字段
            if len(dd.Interact.ErrorHints) > 0 { base.ErrorHints = dd.Interact.ErrorHints } else if len(dd.ErrorHints) > 0 { base.ErrorHints = dd.ErrorHints }
            if len(dd.Interact.AutoInteractions) > 0 {
                mapped := make([]struct{ ExpectOutput, AutoSend string }, 0, len(dd.Interact.AutoInteractions))
                for _, ai := range dd.Interact.AutoInteractions {
                    eo := strings.TrimSpace(ai.ExpectOutput)
                    as := strings.TrimSpace(ai.AutoSend)
                    if eo == "" || as == "" { continue }
                    mapped = append(mapped, struct{ ExpectOutput, AutoSend string }{ExpectOutput: eo, AutoSend: as})
                }
                if len(mapped) > 0 { base.AutoInteractions = mapped }
            } else if len(dd.AutoInteractions) > 0 {
                mapped := make([]struct{ ExpectOutput, AutoSend string }, 0, len(dd.AutoInteractions))
                for _, ai := range dd.AutoInteractions {
                    eo := strings.TrimSpace(ai.ExpectOutput)
                    as := strings.TrimSpace(ai.AutoSend)
                    if eo == "" || as == "" { continue }
                    mapped = append(mapped, struct{ ExpectOutput, AutoSend string }{ExpectOutput: eo, AutoSend: as})
                }
                if len(mapped) > 0 { base.AutoInteractions = mapped }
            }
            base.InteractCaseInsensitive = dd.Interact.CaseInsensitive
            base.InteractTrimSpace = dd.Interact.TrimSpace
        } else if dd, ok := cfg.Collector.DeviceDefaults["default"]; ok {
            if len(dd.PromptSuffixes) > 0 { base.PromptSuffixes = dd.PromptSuffixes }
            base.SkipDelayedEcho = dd.SkipDelayedEcho
            if len(dd.Interact.ErrorHints) > 0 { base.ErrorHints = dd.Interact.ErrorHints } else if len(dd.ErrorHints) > 0 { base.ErrorHints = dd.ErrorHints }
            if len(dd.Interact.AutoInteractions) > 0 {
                mapped := make([]struct{ ExpectOutput, AutoSend string }, 0, len(dd.Interact.AutoInteractions))
                for _, ai := range dd.Interact.AutoInteractions {
                    eo := strings.TrimSpace(ai.ExpectOutput)
                    as := strings.TrimSpace(ai.AutoSend)
                    if eo == "" || as == "" { continue }
                    mapped = append(mapped, struct{ ExpectOutput, AutoSend string }{ExpectOutput: eo, AutoSend: as})
                }
                if len(mapped) > 0 { base.AutoInteractions = mapped }
            } else if len(dd.AutoInteractions) > 0 {
                mapped := make([]struct{ ExpectOutput, AutoSend string }, 0, len(dd.AutoInteractions))
                for _, ai := range dd.AutoInteractions {
                    eo := strings.TrimSpace(ai.ExpectOutput)
                    as := strings.TrimSpace(ai.AutoSend)
                    if eo == "" || as == "" { continue }
                    mapped = append(mapped, struct{ ExpectOutput, AutoSend string }{ExpectOutput: eo, AutoSend: as})
                }
                if len(mapped) > 0 { base.AutoInteractions = mapped }
            }
            base.InteractCaseInsensitive = dd.Interact.CaseInsensitive
            base.InteractTrimSpace = dd.Interact.TrimSpace
        }
    }
    return base
}

// 已移除：平台默认系统采集命令逻辑

// CommandResultView 对外输出的命令结果（包含原始与格式化）
type CommandResultView struct {
	Command      string      `json:"command"`
	RawOutput    string      `json:"raw_output"`
	FormatOutput interface{} `json:"format_output"` // []collect.FormattedRow 或空数组
	Error        string      `json:"error"`
	ExitCode     int         `json:"exit_code"`
	DurationMS   int64       `json:"duration_ms"`
}

// NewCollectorService 创建采集器服务
func NewCollectorService(cfg *config.Config) *CollectorService {
    // 创建SSH连接池配置
    // 并发与线程均由配置/档位应用后的最终值决定
    conc := cfg.Collector.Concurrent
    if conc <= 0 { conc = 1 }
    threads := cfg.Collector.Threads
    if threads <= 0 { threads = cfg.SSH.MaxSessions }
    poolConfig := &ssh.PoolConfig{
        MaxIdle:     10,
        MaxActive:   conc,
        IdleTimeout: 5 * time.Minute,
        SSHConfig: &ssh.Config{
            Timeout:        cfg.SSH.Timeout,
            ConnectTimeout: cfg.SSH.ConnectTimeout,
            KeepAlive:   cfg.SSH.KeepAliveInterval,
            MaxSessions: threads,
        },
    }
    pool := ssh.NewPool(poolConfig)
    return &CollectorService{
        config:   cfg,
        sshPool:  pool,
        interact: NewInteractBasic(cfg, pool),
        tasks:    make(map[string]*TaskContext),
        workers:  make(chan struct{}, conc),
    }
}

// Start 启动采集器服务
func (s *CollectorService) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return fmt.Errorf("collector service is already running")
	}

	s.running = true
	logger.Info("Collector service started")

	// 启动任务清理协程
	go s.cleanupTasks(ctx)

	return nil
}

// Stop 停止采集器服务
func (s *CollectorService) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	// 取消所有正在运行的任务
	for _, taskCtx := range s.tasks {
		if taskCtx.Cancel != nil {
			taskCtx.Cancel()
		}
	}

	// 关闭SSH连接池
	if err := s.sshPool.Close(); err != nil {
		logger.Error("Failed to close SSH pool", "error", err)
	}

	logger.Info("Collector service stopped")
	return nil
}

// ExecuteTask 执行采集任务
func (s *CollectorService) ExecuteTask(ctx context.Context, request *CollectRequest) (*CollectResponse, error) {
	if !s.running {
		return nil, fmt.Errorf("collector service is not running")
	}

	// 在进入工作协程前先解析平台默认与有效超时/重试，用于队列等待控制
	platform := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
	if platform == "" {
		platform = "default"
	}
	// 默认协议
	if proto := strings.TrimSpace(strings.ToLower(request.CollectProtocol)); proto == "" {
		request.CollectProtocol = "ssh"
	}
	if request.CollectProtocol != "ssh" {
		return nil, fmt.Errorf("unsupported collect_protocol: %s", request.CollectProtocol)
	}

	interactDefaults := getPlatformDefaults(platform)
	// 计算有效超时与重试（用于队列等待与任务上下文）
	effTimeout := 30
	if request.Timeout != nil && *request.Timeout > 0 {
		effTimeout = *request.Timeout
	} else if interactDefaults.Timeout > 0 {
		effTimeout = interactDefaults.Timeout
	}
    effRetries := 0
    if request.RetryFlag != nil && *request.RetryFlag >= 0 {
        effRetries = *request.RetryFlag
    } else if interactDefaults.Retries > 0 {
        effRetries = interactDefaults.Retries
    } else if s.config != nil && s.config.Collector.RetryFlags > 0 {
        effRetries = s.config.Collector.RetryFlags
    }

	// 获取工作协程：使用基于有效超时的内部等待上下文，避免HTTP上下文过早结束
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Duration(effTimeout)*time.Second)
	defer waitCancel()
	select {
	case s.workers <- struct{}{}:
		defer func() { <-s.workers }()
	case <-waitCtx.Done():
		return nil, fmt.Errorf("task queue wait timeout after %ds: %w", effTimeout, waitCtx.Err())
	}

	startTime := time.Now()
	response := &CollectResponse{
		TaskID:    request.TaskID,
		Timestamp: startTime,
		Metadata:  request.Metadata,
	}

	// 以上已解析平台与有效超时/重试

	// 构造命令清单：以平台配置为依据，注入必要的预命令（enable、分页关闭），再追加用户命令
	// 注：路由层不注入任何平台默认命令，服务层负责按设备平台动态插入
	commands := make([]string, 0, len(request.CliList)+4)
	// 预命令注入
	preCmds := func() []string {
		out := make([]string, 0, 4)
		p := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
		if p == "" {
			return out
		}
		// 查找设备默认配置
		dd, ok := s.config.Collector.DeviceDefaults[p]
		if !ok {
			if strings.HasPrefix(p, "huawei") {
				dd, ok = s.config.Collector.DeviceDefaults["huawei"]
			} else if strings.HasPrefix(p, "h3c") {
				dd, ok = s.config.Collector.DeviceDefaults["h3c"]
			} else if strings.HasPrefix(p, "cisco") {
				dd, ok = s.config.Collector.DeviceDefaults["cisco_ios"]
			}
		}
		if !ok {
			return out
		}

		// 避免重复：若用户命令里已有相同命令，则不再注入
		hasCmd := func(cmd string) bool {
			key := strings.ToLower(strings.TrimSpace(cmd))
			for _, c := range request.CliList {
				if strings.ToLower(strings.TrimSpace(c)) == key {
					return true
				}
			}
			return false
		}
		// 提权命令注入（如 Cisco enable 或 Linux sudo -i）
		if dd.EnableRequired {
			ecmd := strings.TrimSpace(dd.EnableCLI)
			if ecmd == "" {
				ecmd = "enable"
			}
			if !hasCmd(ecmd) {
				out = append(out, ecmd)
			}
		}
		// 分页关闭命令注入
		for _, pc := range dd.DisablePagingCmds {
			if strings.TrimSpace(pc) == "" {
				continue
			}
			if !hasCmd(pc) {
				out = append(out, pc)
			}
		}
		return out
	}()

	// 拼装最终命令队列：预命令在前，用户命令在后
	if len(preCmds) > 0 {
		commands = append(commands, preCmds...)
	}
	if len(request.CliList) > 0 {
		commands = append(commands, request.CliList...)
	}
	// 命令为空：允许继续（将返回空结果）

	// 记录命令队列
	logger.Info("Prepared command queue", "task_id", request.TaskID, "platform", request.DevicePlatform, "commands", strings.Join(commands, ";"))

	// 创建任务记录
	// 端口默认 22
	port := request.Port
	if port <= 0 || port > 65535 {
		port = 22
	}

	task := &model.Task{
		ID:          request.TaskID,
		CollectorID: s.config.Collector.ID,
		Type:        model.TaskTypeSimple,
		DeviceIP:    request.DeviceIP,
		DevicePort:  port,
		Username:    request.UserName,
		Password:    request.Password,
		Commands:    strings.Join(commands, ";"),
		Status:      model.TaskStatusRunning,
		StartTime:   startTime,
		CreatedAt:   startTime,
		UpdatedAt:   startTime,
	}

	// 保存任务到数据库
	if err := s.saveTask(task); err != nil {
		logger.Error("Failed to save task", "task_id", request.TaskID, "error", err)
	}

	// 创建任务上下文
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(effTimeout)*time.Second)
	defer cancel()

	s.addTaskContext(request.TaskID, &TaskContext{
		Task:      task,
		Cancel:    cancel,
		StartTime: startTime,
		Status:    "running",
	})
	defer s.removeTaskContext(request.TaskID)

	// 执行SSH采集
	execStart := time.Now()
	results, err := s.executeSSHCollection(taskCtx, request, commands, effRetries)
	response.Duration = time.Since(execStart)
	response.DurationMS = response.Duration.Milliseconds()

	if err != nil {
		response.Success = false
		response.Error = err.Error()
		task.Status = model.TaskStatusFailed
		task.ErrorMsg = err.Error()

		// 记录错误日志
		s.logTaskError(request.TaskID, err.Error())
	} else {
		response.Success = true
		response.Results = results
		task.Status = model.TaskStatusSuccess

		// 序列化结果
		if resultData, err := json.Marshal(results); err == nil {
			task.Result = string(resultData)
		}
	}

	// 更新任务状态（以毫秒记录执行时长）
	task.Duration = response.Duration.Milliseconds()
	task.UpdatedAt = time.Now()
	if err := s.updateTask(task); err != nil {
		logger.Error("Failed to update task", "task_id", request.TaskID, "error", err)
	}

	// 已移除 Redis 缓存逻辑

	return response, nil
}

// executeSSHCollection 执行SSH采集
func (s *CollectorService) executeSSHCollection(ctx context.Context, request *CollectRequest, commands []string, retries int) ([]*CommandResultView, error) {
    // 记录开始日志
    port := request.Port
    if port < 1 || port > 65535 { port = 22 }
    s.logTaskInfo(request.TaskID, fmt.Sprintf("Starting SSH collection for %s:%d", request.DeviceIP, port))

    // 计算有效超时（与 ExecuteTask 逻辑保持一致）
    effTimeoutSec := 30
    if request.Timeout != nil && *request.Timeout > 0 {
        effTimeoutSec = *request.Timeout
    } else {
        p := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
        platformKey := p
        if platformKey == "" {
            platformKey = "default"
        }
        d := getPlatformDefaults(platformKey)
        if d.Timeout > 0 { effTimeoutSec = d.Timeout }
    }
    // 统一交互入口：通过 InteractBasic 执行并完成预命令与行过滤
    execReq := &ExecRequest{
        DeviceIP:        request.DeviceIP,
        Port:            port,
        DeviceName:      request.DeviceName,
        DevicePlatform:  request.DevicePlatform,
        CollectProtocol: request.CollectProtocol,
        UserName:        request.UserName,
        Password:        request.Password,
        EnablePassword:  request.EnablePassword,
        TimeoutSec:      effTimeoutSec,
    }

    // 使用请求中的 retries 参数进行重试（至少执行一次）
    attempts := retries
    if attempts < 0 { attempts = 0 }
    maxAttempts := attempts + 1
    var rawResults []*ssh.CommandResult
    var err error
    for i := 0; i < maxAttempts; i++ {
        rawResults, err = s.interact.Execute(ctx, execReq, commands)
        if err == nil {
            if i > 0 {
                s.logTaskInfo(request.TaskID, fmt.Sprintf("Retry successful on attempt %d/%d", i+1, maxAttempts))
            }
            break
        }
        s.logTaskWarn(request.TaskID, fmt.Sprintf("Attempt %d/%d failed: %v", i+1, maxAttempts, err))
        // 若上下文已取消或达到最大重试次数则退出
        if ctx.Err() != nil || i >= attempts {
            break
        }
        // 轻微退避，避免立即重试造成设备压力
        time.Sleep(time.Duration(150*(i+1)) * time.Millisecond)
    }
    if err != nil {
        return nil, err
    }
    // 记录成功日志
    s.logTaskInfo(request.TaskID, fmt.Sprintf("SSH collection completed, executed %d commands", len(rawResults)))

    // 格式化解析
    platform := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
    if platform == "" { platform = "default" }

	// 显示用原始命令队列：路由层已决定是否预组装系统命令，这里直接回显请求中的命令
	displayCmds := func() []string {
		out := make([]string, 0, len(request.CliList))
		if len(request.CliList) > 0 {
			out = append(out, request.CliList...)
		}
		return out
	}()

    out := make([]*CommandResultView, 0, len(rawResults))
	// 解析模式：不再依赖 origin，改为从 metadata.collect_mode 派生
	collectMode := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", request.Metadata["collect_mode"])))
	if collectMode == "" {
		collectMode = "customer"
	}
    dispIdx := 0
    for _, r := range rawResults {
        // 防御式：r 可能为 nil（例如连接被 keepalive 标记为断开导致 ExecuteCommand 返回 nil）
        cmdVal := ""
        if r != nil {
            cmdVal = r.Command
        }
		// 显示层使用原始命令映射；解析层继续用规范化命令
		displayCmd := cmdVal
		if dispIdx < len(displayCmds) {
			displayCmd = displayCmds[dispIdx]
		}
		dispIdx++
		// 当前不进行结构化解析，保持空数组以兼容 API 字段
		var fmtRows interface{} = []map[string]interface{}{}
        // 错误提示检测：如配置了 error_hints，当输出行以提示前缀开头时标记错误
        detectedErr := ""
        if r != nil && r.Error == "" && collectMode != "customer" {
            // 错误提示基于平台/默认平台配置，不再叠加全局
            defaults := getPlatformDefaults(platform)
            hints := defaults.ErrorHints
            if len(hints) > 0 {
                raw := r.Output
                lines := strings.Split(raw, "\n")
                for _, ln := range lines {
                    t := ln
                    if defaults.InteractTrimSpace {
                        t = strings.TrimSpace(t)
                    }
                    cmp := t
                    if defaults.InteractCaseInsensitive {
                        cmp = strings.ToLower(cmp)
                    }
                    for _, h := range hints {
                        hh := h
                        if defaults.InteractTrimSpace {
                            hh = strings.TrimSpace(hh)
                        }
                        if defaults.InteractCaseInsensitive {
                            hh = strings.ToLower(hh)
                        }
                        if hh != "" && strings.HasPrefix(cmp, hh) {
                            detectedErr = fmt.Sprintf("command error hint matched: %s", t)
                            break
                        }
                    }
                    if detectedErr != "" {
                        break
                    }
                }
            }
        }
        // 如命中了错误提示，记录任务警告日志（非致命）
        if detectedErr != "" {
            s.logTaskWarn(request.TaskID, fmt.Sprintf("command hint matched for %q: %s", displayCmd, detectedErr))
        }
        // 计算过滤统计与错误传播标记
        var rawStripped string
        var beforeLines, afterLines int
        var exitCodeVal int
        var durationMsVal int64
        var errorVal string
        propagated := false
        if r != nil {
            // 输出已由统一入口过滤，这里直接使用
            rawStripped = r.Output
            beforeLines = len(strings.Split(r.Output, "\n"))
            afterLines = len(strings.Split(rawStripped, "\n"))
            exitCodeVal = r.ExitCode
            durationMsVal = int64(r.Duration / time.Millisecond)
            if r.Error != "" {
                errorVal = r.Error
            } else if detectedErr != "" {
                errorVal = detectedErr
            }
        } else {
            exitCodeVal = -1
            durationMsVal = 0
            rawStripped = ""
        }

		view := &CommandResultView{
			Command:      displayCmd,
			RawOutput:    rawStripped,
			FormatOutput: fmtRows,
			Error:        errorVal,
			ExitCode:     exitCodeVal,
			DurationMS:   durationMsVal,
		}

        logger.Debugf("Collector output filter: cmd=%q lines_before=%d lines_after=%d exit=%d dur_ms=%d error_propagated=%v", displayCmd, beforeLines, afterLines, exitCodeVal, durationMsVal, propagated)
        out = append(out, view)
    }

    return out, nil
}

// GetTaskStatus 获取任务状态
func (s *CollectorService) GetTaskStatus(taskID string) (*TaskContext, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if taskCtx, exists := s.tasks[taskID]; exists {
		return taskCtx, nil
	}

	return nil, fmt.Errorf("task not found: %s", taskID)
}

// CancelTask 取消任务
func (s *CollectorService) CancelTask(taskID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if taskCtx, exists := s.tasks[taskID]; exists {
		if taskCtx.Cancel != nil {
			taskCtx.Cancel()
			taskCtx.Status = "cancelled"
		}
		return nil
	}

	return fmt.Errorf("task not found: %s", taskID)
}

// GetStats 获取采集器统计信息
func (s *CollectorService) GetStats() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := map[string]interface{}{
		"running":      s.running,
		"active_tasks": len(s.tasks),
		"max_workers":  cap(s.workers),
		"busy_workers": len(s.workers),
		"ssh_pool":     s.sshPool.GetStats(),
	}

	return stats
}

// addTaskContext 添加任务上下文
func (s *CollectorService) addTaskContext(taskID string, taskCtx *TaskContext) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.tasks[taskID] = taskCtx
}

// removeTaskContext 移除任务上下文
func (s *CollectorService) removeTaskContext(taskID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.tasks, taskID)
}

// cleanupTasks 清理过期任务
func (s *CollectorService) cleanupTasks(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpiredTasks()
		}
	}
}

// cleanupExpiredTasks 清理过期任务
func (s *CollectorService) cleanupExpiredTasks() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	toDelete := make([]string, 0)

	for taskID, taskCtx := range s.tasks {
		// 清理超过1小时的任务
		if now.Sub(taskCtx.StartTime) > time.Hour {
			toDelete = append(toDelete, taskID)
		}
	}

	for _, taskID := range toDelete {
		delete(s.tasks, taskID)
	}
}

// saveTask 保存任务到数据库
func (s *CollectorService) saveTask(task *model.Task) error {
	// 如果主键已存在则进行更新（upsert），避免重复任务ID导致插入失败
	return database.WithRetry(func(db *gorm.DB) error {
		return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(task).Error
	}, 5, 50*time.Millisecond)
}

// updateTask 更新任务状态
func (s *CollectorService) updateTask(task *model.Task) error {
	return database.WithRetry(func(db *gorm.DB) error {
		return db.Save(task).Error
	}, 5, 50*time.Millisecond)
}

// 已移除 Redis 缓存函数

// logTaskInfo 记录任务信息日志
func (s *CollectorService) logTaskInfo(taskID, message string) {
	logger.Info("Task info", "task_id", taskID, "message", message)
	s.saveTaskLog(taskID, "INFO", message)
}

// logTaskError 记录任务错误日志
func (s *CollectorService) logTaskError(taskID, message string) {
	logger.Error("Task error", "task_id", taskID, "message", message)
	s.saveTaskLog(taskID, "ERROR", message)
}

// logTaskWarn 记录任务警告日志
func (s *CollectorService) logTaskWarn(taskID, message string) {
	logger.Warn("Task warn", "task_id", taskID, "message", message)
	s.saveTaskLog(taskID, "WARN", message)
}

// saveTaskLog 保存任务日志
func (s *CollectorService) saveTaskLog(taskID, level, message string) {
	taskLog := &model.TaskLog{
		ID:        uuid.NewString(),
		TaskID:    taskID,
		Level:     level,
		Message:   message,
		CreatedAt: time.Now(),
	}

	if err := database.WithRetry(func(db *gorm.DB) error {
		return db.Create(taskLog).Error
	}, 5, 50*time.Millisecond); err != nil {
		logger.Warn("Failed to save task log", "task_id", taskID, "error", err)
	}
}
