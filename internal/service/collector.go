package service

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
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
}

// getPlatformDefaults 返回平台内置交互默认值（不再依赖插件）
func getPlatformDefaults(platform string) platformInteractDefaults {
    p := strings.TrimSpace(strings.ToLower(platform))

    // 先确定内置平台默认
    base := platformInteractDefaults{}
    switch p {
    case "cisco_ios":
        base = platformInteractDefaults{
            Timeout:           60,
            Retries:           2,
            Threads:           4,
            Concurrent:        5,
            PromptSuffixes:    []string{">", "#"},
            CommandIntervalMS: 200,
            AutoInteractions: []struct{ ExpectOutput, AutoSend string }{
                {ExpectOutput: "--more--", AutoSend: " "},
                {ExpectOutput: "more", AutoSend: " "},
                {ExpectOutput: "press any key", AutoSend: " "},
                {ExpectOutput: "confirm", AutoSend: "y"},
                {ExpectOutput: "[yes/no]", AutoSend: "yes"},
            },
            ErrorHints:      []string{"invalid input detected", "incomplete command", "ambiguous command", "unknown command", "invalid autocommand", "line has invalid autocommand"},
            SkipDelayedEcho: true,
        }
    case "huawei_s", "huawei_ce":
        base = platformInteractDefaults{
            Timeout:           60,
            Retries:           2,
            Threads:           4,
            Concurrent:        5,
            PromptSuffixes:    []string{">", "#", "]"},
            CommandIntervalMS: 300,
            AutoInteractions: []struct{ ExpectOutput, AutoSend string }{
                {ExpectOutput: "more", AutoSend: " "},
                {ExpectOutput: "press any key", AutoSend: " "},
                {ExpectOutput: "confirm", AutoSend: "y"},
            },
            ErrorHints:      []string{"error:", "unrecognized command"},
            SkipDelayedEcho: true,
        }
    case "h3c_s", "h3c_sr", "h3c_msr":
        base = platformInteractDefaults{
            Timeout:           75,
            Retries:           2,
            Threads:           4,
            Concurrent:        5,
            PromptSuffixes:    []string{">", "#", "]"},
            CommandIntervalMS: 150,
            AutoInteractions: []struct{ ExpectOutput, AutoSend string }{
                {ExpectOutput: "more", AutoSend: " "},
                {ExpectOutput: "press any key", AutoSend: " "},
            },
            ErrorHints:      []string{"error:", "unrecognized command"},
            SkipDelayedEcho: true,
        }
    default:
        base = platformInteractDefaults{
            Timeout:           30,
            Retries:           1,
            Threads:           4,
            Concurrent:        5,
            PromptSuffixes:    []string{"#", ">", "]"},
            CommandIntervalMS: 150,
            AutoInteractions: []struct{ ExpectOutput, AutoSend string }{
                {ExpectOutput: "more", AutoSend: " "},
                {ExpectOutput: "press any key", AutoSend: " "},
                {ExpectOutput: "confirm", AutoSend: "y"},
            },
            ErrorHints:      []string{"error", "invalid", "unrecognized", "incomplete", "ambiguous"},
            SkipDelayedEcho: true,
        }
    }

    // 再从配置进行覆盖（collector.device_defaults）
    if cfg := config.Get(); cfg != nil {
        key := p
        dd, ok := cfg.Collector.DeviceDefaults[key]
        if !ok {
            // 合并家族键
            if strings.HasPrefix(p, "huawei") {
                dd, ok = cfg.Collector.DeviceDefaults["huawei"]
            } else if strings.HasPrefix(p, "h3c") {
                dd, ok = cfg.Collector.DeviceDefaults["h3c"]
            }
        }
        if ok {
            if len(dd.PromptSuffixes) > 0 {
                base.PromptSuffixes = dd.PromptSuffixes
            }
            // 覆盖跳过延迟回显
            base.SkipDelayedEcho = dd.SkipDelayedEcho
            if len(dd.ErrorHints) > 0 {
                base.ErrorHints = dd.ErrorHints
            }
            if len(dd.AutoInteractions) > 0 {
                mapped := make([]struct{ ExpectOutput, AutoSend string }, 0, len(dd.AutoInteractions))
                for _, ai := range dd.AutoInteractions {
                    eo := strings.TrimSpace(ai.ExpectOutput)
                    as := strings.TrimSpace(ai.AutoSend)
                    if eo == "" || as == "" {
                        continue
                    }
                    mapped = append(mapped, struct{ ExpectOutput, AutoSend string }{ExpectOutput: eo, AutoSend: as})
                }
                if len(mapped) > 0 {
                    base.AutoInteractions = mapped
                }
            }
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

	return &CollectorService{
		config:  cfg,
		sshPool: ssh.NewPool(poolConfig),
		tasks:   make(map[string]*TaskContext),
		workers: make(chan struct{}, cfg.Collector.Concurrent),
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
	// 创建SSH连接信息
	connInfo := &ssh.ConnectionInfo{
		Host: request.DeviceIP,
		Port: func() int {
			if request.Port < 1 || request.Port > 65535 {
				return 22
			}
			return request.Port
		}(),
		Username: request.UserName,
		Password: request.Password,
	}

	// 记录开始日志（使用连接信息中的端口）
	s.logTaskInfo(request.TaskID, fmt.Sprintf("Starting SSH collection for %s:%d", request.DeviceIP, connInfo.Port))

	// 单次登录：获取一次连接
	client, err := s.sshPool.GetConnection(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH connection: %w", err)
	}
	defer s.sshPool.ReleaseConnection(connInfo)

	// 系统默认：使用单一交互式会话(PTY)执行整批命令
	// 使用内置平台默认值配置提示符与交互参数
	platform := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
	defaults := getPlatformDefaults(func() string {
		if platform == "" {
			return "default"
		}
		return platform
	}())
	promptSuffixes := defaults.PromptSuffixes
	if len(promptSuffixes) == 0 {
		// 通用提示符后缀集合，统一交互逻辑，避免过度平台分支
		promptSuffixes = []string{"#", ">", "]"}
	}

    // 在交互会话中自动处理提权密码
    // 配置交互选项：平台特定退出命令与可选 enable 密码/提示
    interactiveOpts := &ssh.InteractiveOptions{}
    if strings.HasPrefix(platform, "cisco") {
        interactiveOpts.ExitCommands = []string{"exit"}
    } else if strings.HasPrefix(platform, "h3c") || strings.HasPrefix(platform, "huawei") {
        interactiveOpts.ExitCommands = []string{"quit", "exit"}
    } else {
        interactiveOpts.ExitCommands = []string{"exit", "quit"}
    }
    // 处理设备级提权配置：当 enable_required 为 true 时启用提权命令与密码
    {
        dd, ok := s.config.Collector.DeviceDefaults[platform]
        if !ok {
            if strings.HasPrefix(platform, "huawei") {
                dd, ok = s.config.Collector.DeviceDefaults["huawei"]
            } else if strings.HasPrefix(platform, "h3c") {
                dd, ok = s.config.Collector.DeviceDefaults["h3c"]
            } else if strings.HasPrefix(platform, "cisco") {
                dd, ok = s.config.Collector.DeviceDefaults["cisco_ios"]
            }
        }
        if ok && dd.EnableRequired {
            // 设置提权命令与提示匹配
            interactiveOpts.EnableCLI = strings.TrimSpace(dd.EnableCLI)
            interactiveOpts.EnableExpectOutput = strings.TrimSpace(dd.EnableExceptOutput)
            // 优先使用请求中的 enable 密码；否则回退为登录密码
            if strings.TrimSpace(request.EnablePassword) != "" {
                interactiveOpts.EnablePassword = strings.TrimSpace(request.EnablePassword)
            } else if strings.TrimSpace(request.Password) != "" {
                interactiveOpts.EnablePassword = strings.TrimSpace(request.Password)
            }
        } else if strings.HasPrefix(platform, "cisco") {
            // 兼容旧逻辑：Cisco 未显式配置时仍尝试使用 enable
            if strings.TrimSpace(request.EnablePassword) != "" {
                interactiveOpts.EnablePassword = strings.TrimSpace(request.EnablePassword)
            } else if strings.TrimSpace(request.Password) != "" {
                interactiveOpts.EnablePassword = strings.TrimSpace(request.Password)
            }
            if strings.TrimSpace(interactiveOpts.EnableCLI) == "" {
                interactiveOpts.EnableCLI = "enable"
            }
            if strings.TrimSpace(interactiveOpts.EnableExpectOutput) == "" {
                interactiveOpts.EnableExpectOutput = "Password"
            }
        }
    }
	// 应用内置默认的命令间隔与自动交互配置
	if defaults.CommandIntervalMS > 0 {
		interactiveOpts.CommandIntervalMS = defaults.CommandIntervalMS
	}
	if len(defaults.AutoInteractions) > 0 {
		mapped := make([]ssh.AutoInteraction, 0, len(defaults.AutoInteractions))
		for _, ai := range defaults.AutoInteractions {
			if strings.TrimSpace(ai.ExpectOutput) == "" || strings.TrimSpace(ai.AutoSend) == "" {
				continue
			}
			mapped = append(mapped, ssh.AutoInteraction{ExpectOutput: ai.ExpectOutput, AutoSend: ai.AutoSend})
		}
		interactiveOpts.AutoInteractions = mapped
	}
	// 按平台默认启用/关闭“延迟回显跳过”
	interactiveOpts.SkipDelayedEcho = defaults.SkipDelayedEcho
    // 合并配置层的 auto_interactions：在默认/设备级基础上追加或覆盖
    if cfgAIs := s.config.Collector.Interact.AutoInteractions; len(cfgAIs) > 0 {
        // 建立索引以按 ExpectOutput 覆盖
        idx := map[string]int{}
        for i, ai := range interactiveOpts.AutoInteractions {
            key := strings.ToLower(strings.TrimSpace(ai.ExpectOutput))
            if key != "" {
                idx[key] = i
            }
        }
        // 追加或覆盖配置的自动交互项
        for _, ai := range cfgAIs {
            eo := strings.TrimSpace(ai.ExpectOutput)
            as := strings.TrimSpace(ai.AutoSend)
            if eo == "" || as == "" {
                continue
            }
            key := strings.ToLower(eo)
            if pos, ok := idx[key]; ok {
                // 覆盖现有项
                interactiveOpts.AutoInteractions[pos] = ssh.AutoInteraction{ExpectOutput: eo, AutoSend: as}
            } else {
                interactiveOpts.AutoInteractions = append(interactiveOpts.AutoInteractions, ssh.AutoInteraction{ExpectOutput: eo, AutoSend: as})
                idx[key] = len(interactiveOpts.AutoInteractions) - 1
            }
        }
    }
    // 增加重试：根据 retries 参数对交互式执行进行有限次重试
    var rawResults []*ssh.CommandResult
    for attempt := 0; attempt <= retries; attempt++ {
        rawResults, err = client.ExecuteInteractiveCommands(ctx, commands, promptSuffixes, interactiveOpts)
        if err == nil {
            break
        }
        s.logTaskWarn(request.TaskID, fmt.Sprintf("interactive attempt %d/%d failed: %v", attempt+1, retries+1, err))
        // 短暂退避，避免设备端限流或会话抖动
        time.Sleep(200 * time.Millisecond)
    }
    if err != nil {
        // 交互式失败：先重置连接，再进行非交互回退，避免复用异常连接导致 "disconnect message type 97"
        _ = s.sshPool.CloseConnection(connInfo)
        // 尝试重新获取新连接（使用同一上下文预算）
        client, _ = s.sshPool.GetConnection(ctx, connInfo)

        tmp := make([]*ssh.CommandResult, 0, len(commands))
        successAny := false
        for _, cmd := range commands {
            res, e := client.ExecuteCommand(ctx, cmd)
            if e != nil {
                s.logTaskError(request.TaskID, fmt.Sprintf("fallback exec failed for '%s': %v", cmd, e))
            } else if res != nil {
                successAny = true
            }
            tmp = append(tmp, res)
        }
        if successAny {
            // 回退成功，记录警告而非错误，避免误判任务失败
            s.logTaskWarn(request.TaskID, fmt.Sprintf("interactive session failed; used non-interactive fallback: %v", err))
            rawResults = tmp
        } else {
            // 回退也失败，保留原始错误
            s.logTaskError(request.TaskID, fmt.Sprintf("interactive session failed and fallback produced no output: %v", err))
            return nil, err
        }
    }

	// Cisco 平台：在内部命令过滤前检查 enable 结果，若未进入 '#' 模式则记录错误，并在后续结果中进行提示传播
    enableFailedMsg := ""
    {
        dd, ok := s.config.Collector.DeviceDefaults[platform]
        if !ok {
            if strings.HasPrefix(platform, "huawei") {
                dd, ok = s.config.Collector.DeviceDefaults["huawei"]
            } else if strings.HasPrefix(platform, "h3c") {
                dd, ok = s.config.Collector.DeviceDefaults["h3c"]
            } else if strings.HasPrefix(platform, "cisco") {
                dd, ok = s.config.Collector.DeviceDefaults["cisco_ios"]
            }
        }
        enableCmd := strings.TrimSpace(dd.EnableCLI)
        if enableCmd == "" {
            enableCmd = "enable"
        }
        for _, r := range rawResults {
            if r == nil {
                continue
            }
            if strings.EqualFold(strings.TrimSpace(r.Command), enableCmd) {
                if r.ExitCode != 0 || strings.TrimSpace(r.Error) != "" {
                    enableFailedMsg = strings.TrimSpace(r.Error)
                    if enableFailedMsg == "" {
                        enableFailedMsg = "enable did not reach privileged prompt (#); still in user mode"
                    }
                    s.logTaskError(request.TaskID, fmt.Sprintf("Enable mode not entered: %s", enableFailedMsg))
                }
                break
            }
        }
    }

	// 记录成功日志
	s.logTaskInfo(request.TaskID, fmt.Sprintf("SSH collection completed, executed %d commands", len(rawResults)))

	// 格式化解析
	platform = strings.TrimSpace(strings.ToLower(request.DevicePlatform))
    if platform == "" {
        platform = "default"
    }
    // 外部采集插件已移除，不进行插件解析

    // 显示用原始命令队列：路由层已决定是否预组装系统命令，这里直接回显请求中的命令
    displayCmds := func() []string {
        out := make([]string, 0, len(request.CliList))
        if len(request.CliList) > 0 {
            out = append(out, request.CliList...)
        }
        return out
    }()

    // 过滤掉服务层注入的“内部预命令”，使其不体现在最终结果中
    // 动态来源：配置中的 device_defaults（enable、分页关闭命令）
    internalCmds := map[string]struct{}{}
    {
        dd, ok := s.config.Collector.DeviceDefaults[platform]
        if !ok {
            if strings.HasPrefix(platform, "huawei") {
                dd, ok = s.config.Collector.DeviceDefaults["huawei"]
            } else if strings.HasPrefix(platform, "h3c") {
                dd, ok = s.config.Collector.DeviceDefaults["h3c"]
            } else if strings.HasPrefix(platform, "cisco") {
                dd, ok = s.config.Collector.DeviceDefaults["cisco_ios"]
            }
        }
        if ok {
            if dd.EnableRequired {
                ecmd := strings.TrimSpace(dd.EnableCLI)
                if ecmd == "" {
                    ecmd = "enable"
                }
                internalCmds[strings.ToLower(strings.TrimSpace(ecmd))] = struct{}{}
            }
            for _, pc := range dd.DisablePagingCmds {
                if strings.TrimSpace(pc) == "" {
                    continue
                }
                internalCmds[strings.ToLower(strings.TrimSpace(pc))] = struct{}{}
            }
        }
    }

	filtered := make([]*ssh.CommandResult, 0, len(rawResults))
	for _, r := range rawResults {
		if r == nil {
			filtered = append(filtered, r)
			continue
		}
		cmdKey := strings.ToLower(strings.TrimSpace(r.Command))
		if _, isInternal := internalCmds[cmdKey]; isInternal {
			// 跳过内部预命令，不进入最终输出
			continue
		}
		filtered = append(filtered, r)
	}

    out := make([]*CommandResultView, 0, len(filtered))
    // 解析模式：不再依赖 origin，改为从 metadata.collect_mode 派生
    collectMode := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", request.Metadata["collect_mode"])))
    if collectMode == "" {
        collectMode = "customer"
    }
	// 辅助：根据配置移除原始输出中的分页提示等行
	stripPagerPrompts := func(src string) string {
		if src == "" {
			return src
		}
		// 读取配置规则
		of := s.config.Collector.OutputFilter
		// 预处理规则（统一大小写与空格）
		normalize := func(x string, trim, ci bool) string {
			if trim {
				x = strings.TrimSpace(x)
			}
			if ci {
				x = strings.ToLower(x)
			}
			return x
		}
		pref := make([]string, 0, len(of.Prefixes))
		for _, p := range of.Prefixes {
			// 模式本身也做 trim + 可选大小写折叠
			pref = append(pref, normalize(p, true, of.CaseInsensitive))
		}
		subs := make([]string, 0, len(of.Contains))
		for _, c := range of.Contains {
			subs = append(subs, normalize(c, true, of.CaseInsensitive))
		}

		lines := strings.Split(src, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			cmp := normalize(line, of.TrimSpace, of.CaseInsensitive)
			// 前缀匹配
			matched := false
			for _, p := range pref {
				if p == "" {
					continue
				}
				if strings.HasPrefix(cmp, p) {
					matched = true
					break
				}
			}
			if !matched {
				// 包含匹配
				for _, c := range subs {
					if c == "" {
						continue
					}
					if strings.Contains(cmp, c) {
						matched = true
						break
					}
				}
			}
			if matched {
				continue
			}
			out = append(out, line)
		}
		return strings.Join(out, "\n")
	}

	dispIdx := 0
	for _, r := range filtered {
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
        // 合并平台默认错误提示：优先使用配置的 error_hints，并追加平台默认
        // 以确保诸如 Cisco "invalid autocommand" 之类的提示能够被识别
        merged := make([]string, 0, len(s.config.Collector.Interact.ErrorHints)+len(defaults.ErrorHints))
			// 先加入配置中的 hints
			merged = append(merged, s.config.Collector.Interact.ErrorHints...)
			// 再追加平台默认 hints（去重）
			seen := map[string]struct{}{}
			for _, h := range merged {
				seen[h] = struct{}{}
			}
			for _, h := range defaults.ErrorHints {
				if _, ok := seen[h]; !ok {
					merged = append(merged, h)
					seen[h] = struct{}{}
				}
			}
			hints := merged
			if len(hints) > 0 {
				raw := r.Output
				lines := strings.Split(raw, "\n")
				for _, ln := range lines {
					t := ln
					if s.config.Collector.Interact.TrimSpace {
						t = strings.TrimSpace(t)
					}
					cmp := t
					if s.config.Collector.Interact.CaseInsensitive {
						cmp = strings.ToLower(cmp)
					}
					for _, h := range hints {
						hh := h
						if s.config.Collector.Interact.TrimSpace {
							hh = strings.TrimSpace(hh)
						}
						if s.config.Collector.Interact.CaseInsensitive {
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
		view := &CommandResultView{
			Command: displayCmd,
			RawOutput: func() string {
				if r != nil {
					return stripPagerPrompts(r.Output)
				}
				return ""
			}(),
			FormatOutput: fmtRows,
			Error: func() string {
				if r != nil {
					// 传播 enable 失败提示到后续命令（仅 Cisco）
					if strings.HasPrefix(platform, "cisco") && enableFailedMsg != "" {
						if r.Error != "" {
							return r.Error
						}
						if detectedErr != "" {
							return detectedErr
						}
						return fmt.Sprintf("privileged mode not entered (#): %s", enableFailedMsg)
					}
					if r.Error != "" {
						return r.Error
					}
					if detectedErr != "" {
						return detectedErr
					}
					return ""
				}
				return ""
			}(),
			ExitCode: func() int {
				if r != nil {
					return r.ExitCode
				}
				return -1
			}(),
			DurationMS: func() int64 {
				if r != nil {
					return int64(r.Duration / time.Millisecond)
				}
				return 0
			}(),
		}
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
	db := database.GetDB()
	// 如果主键已存在则进行更新（upsert），避免重复任务ID导致插入失败
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(task).Error
}

// updateTask 更新任务状态
func (s *CollectorService) updateTask(task *model.Task) error {
	db := database.GetDB()
	return db.Save(task).Error
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
	db := database.GetDB()
	taskLog := &model.TaskLog{
		ID:        uuid.NewString(),
		TaskID:    taskID,
		Level:     level,
		Message:   message,
		CreatedAt: time.Now(),
	}

	if err := db.Create(taskLog).Error; err != nil {
		logger.Error("Failed to save task log", "task_id", taskID, "error", err)
	}
}
