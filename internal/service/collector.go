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

	"github.com/sshcollectorpro/sshcollectorpro/addone/collect"
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
    switch p {
    case "cisco_ios":
        return platformInteractDefaults{
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
        return platformInteractDefaults{
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
        return platformInteractDefaults{
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
        return platformInteractDefaults{
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
}

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

	// 获取工作协程
	select {
	case s.workers <- struct{}{}:
		defer func() { <-s.workers }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	startTime := time.Now()
	response := &CollectResponse{
		TaskID:    request.TaskID,
		Timestamp: startTime,
		Metadata:  request.Metadata,
	}

	// 解析交互与超时/重试默认值
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

    // 内置交互默认值（不依赖插件）
    interactDefaults := getPlatformDefaults(platform)
	// 计算有效超时与重试
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

	// 构造命令清单（system/customer）
	commands := make([]string, 0)
	origin := strings.TrimSpace(strings.ToLower(request.CollectOrigin))
	if origin == "" {
		origin = "customer"
	}
	if origin == "system" {
		// 系统任务需要平台内置命令
		cpl := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
		if cpl == "" || cpl == "default" {
			return nil, fmt.Errorf("collect_origin=system requires device_platform")
		}
		plugin := collect.Get(cpl)
		commands = append(commands, plugin.SystemCommands()...)
		// 如果用户指定了 cli_list，则在系统命令之后追加（允许扩展）
		if len(request.CliList) > 0 {
			commands = append(commands, request.CliList...)
		}
	} else {
		// customer：使用用户提供的命令
		commands = append(commands, request.CliList...)
	}
	// 命令为空处理：customer 可为空返回空结果；system 为空则报错
	if len(commands) == 0 {
		if origin == "system" {
			return nil, fmt.Errorf("cli_list is empty and no system commands available")
		}
		// customer：允许空命令，继续后续流程（将返回空结果）
	}

    // 内置命令转换（如特权与取消分页）
    // Cisco：插入 enable/terminal length 0，并规范化 show run*
    // Huawei/H3C：插入 screen-length disable
    switch strings.TrimSpace(strings.ToLower(request.DevicePlatform)) {
    case "cisco_ios":
        // 前置命令：仅当提供了 enable_password 才插入 enable；取消分页始终插入
        pre := make([]string, 0, 2)
        if strings.TrimSpace(request.EnablePassword) != "" {
            pre = append(pre, "enable")
        }
        pre = append(pre, "terminal length 0")

        // 保持用户原始输入命令，不做映射或归一化
        original := make([]string, 0, len(commands))
        for _, c := range commands {
            original = append(original, c)
        }
        out := make([]string, 0, len(pre)+len(original))
        out = append(out, pre...)
        out = append(out, original...)
        commands = out
        logger.Info("Prepared command queue", "task_id", request.TaskID, "platform", request.DevicePlatform, "commands", strings.Join(commands, ";"))
    case "huawei_s", "huawei_ce", "h3c_s", "h3c_sr", "h3c_msr":
        // 取消分页
        pre := []string{"screen-length disable"}
        out := make([]string, 0, len(pre)+len(commands))
        out = append(out, pre...)
        out = append(out, commands...)
        commands = out
        logger.Info("Prepared command queue", "task_id", request.TaskID, "platform", request.DevicePlatform, "commands", strings.Join(commands, ";"))
    default:
        // 默认不做转换
        logger.Info("Prepared command queue", "task_id", request.TaskID, "platform", request.DevicePlatform, "commands", strings.Join(commands, ";"))
    }

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
        promptSuffixes = []string{"#", ">", "]"}
        if strings.HasPrefix(platform, "cisco") {
            // Cisco 设备常见有两种提示符：用户 EXEC 模式 '>' 与特权模式 '#'
            // 为避免在进入 enable 前无法识别提示符，包含两者
            promptSuffixes = []string{">", "#"}
        } else if strings.HasPrefix(platform, "h3c") || strings.HasPrefix(platform, "huawei") {
            promptSuffixes = []string{">"}
        }
    }

    // 在交互会话中自动处理 Cisco enable 密码
    // 配置交互选项：平台特定退出命令与可选 enable 密码
    interactiveOpts := &ssh.InteractiveOptions{}
    if strings.HasPrefix(platform, "cisco") {
        interactiveOpts.ExitCommands = []string{"exit"}
        // 优先使用请求中的 enable 密码
        if strings.TrimSpace(request.EnablePassword) != "" {
            interactiveOpts.EnablePassword = strings.TrimSpace(request.EnablePassword)
        } else {
            // 兼容性回退：若未提供 enable 密码，尝试使用登录密码作为 enable 密码
            // 许多环境中登录密码与 enable 密码一致，避免无法进入特权模式
            if strings.TrimSpace(request.Password) != "" {
                interactiveOpts.EnablePassword = strings.TrimSpace(request.Password)
            }
        }
    } else if strings.HasPrefix(platform, "h3c") || strings.HasPrefix(platform, "huawei") {
        interactiveOpts.ExitCommands = []string{"quit", "exit"}
    } else {
        interactiveOpts.ExitCommands = []string{"exit", "quit"}
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
	// 配置覆盖：如在配置文件中定义了 auto_interactions，则优先使用配置（覆盖插件默认）
	if cfgAIs := s.config.Collector.Interact.AutoInteractions; len(cfgAIs) > 0 {
		mapped := make([]ssh.AutoInteraction, 0, len(cfgAIs))
		for _, ai := range cfgAIs {
			if strings.TrimSpace(ai.ExpectOutput) == "" || strings.TrimSpace(ai.AutoSend) == "" {
				continue
			}
			mapped = append(mapped, ssh.AutoInteraction{ExpectOutput: ai.ExpectOutput, AutoSend: ai.AutoSend})
		}
		if len(mapped) > 0 {
			interactiveOpts.AutoInteractions = mapped
		}
	}
	rawResults, err := client.ExecuteInteractiveCommands(ctx, commands, promptSuffixes, interactiveOpts)
	if err != nil {
		s.logTaskError(request.TaskID, fmt.Sprintf("interactive session failed: %v", err))
		// 回退：如果交互式失败且允许重试，尝试非交互按条执行
		if retries > 0 {
			tmp := make([]*ssh.CommandResult, 0, len(commands))
			for _, cmd := range commands {
				res, e := client.ExecuteCommand(ctx, cmd)
				if e != nil {
					s.logTaskError(request.TaskID, fmt.Sprintf("fallback exec failed for '%s': %v", cmd, e))
				}
				tmp = append(tmp, res)
			}
			rawResults = tmp
		} else {
			return nil, err
		}
	}

    // Cisco 平台：在内部命令过滤前检查 enable 结果，若未进入 '#' 模式则记录错误，并在后续结果中进行提示传播
    enableFailedMsg := ""
    if strings.HasPrefix(platform, "cisco") {
        for _, r := range rawResults {
            if r == nil {
                continue
            }
            if strings.EqualFold(strings.TrimSpace(r.Command), "enable") {
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
	plugin := collect.Get(platform)

	// 显示用原始命令队列（保持解析用规范化、展示用原始输入）
	displayCmds := func() []string {
		origin := strings.TrimSpace(strings.ToLower(request.CollectOrigin))
		if origin == "" {
			origin = "customer"
		}
		out := make([]string, 0)
		if origin == "system" {
			cpl := strings.TrimSpace(strings.ToLower(request.DevicePlatform))
			if cpl != "" && cpl != "default" {
				p := collect.Get(cpl)
				out = append(out, p.SystemCommands()...)
			} else {
				// default 平台无系统命令，保留传入的 cli_list
			}
			if len(request.CliList) > 0 {
				out = append(out, request.CliList...)
			}
			return out
		}
		// customer：直接使用用户提供的命令
		return append(out, request.CliList...)
	}()

	// 过滤掉由交互插件注入的“内部预命令”，使其不体现在最终结果中
	// 例如：cisco_ios → enable、terminal length 0；华为/H3C → screen-length disable
	internalCmds := map[string]struct{}{}
	switch {
	case strings.HasPrefix(platform, "cisco"):
		for _, p := range []string{"enable", "terminal length 0"} {
			internalCmds[strings.ToLower(strings.TrimSpace(p))] = struct{}{}
		}
	case strings.HasPrefix(platform, "huawei"), strings.HasPrefix(platform, "h3c"):
		internalCmds[strings.ToLower(strings.TrimSpace("screen-length disable"))] = struct{}{}
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
	origin := strings.TrimSpace(strings.ToLower(request.CollectOrigin))
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
	
	// 控制是否传播“未进入特权模式”的统一提示：当后续任何命令检测到 '#' 后停止传播
	propagateEnableFailed := strings.HasPrefix(platform, "cisco") && enableFailedMsg != ""
	
	dispIdx := 0
	for _, r := range filtered {
		status := model.TaskStatusSuccess
		if r == nil || r.ExitCode != 0 {
			status = model.TaskStatusFailed
		}
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
		ctxParse := collect.ParseContext{
			Platform: platform,
			Command:  cmdVal,
			TaskID:   request.TaskID,
			Status:   status,
			RawPaths: make(collect.RawStorePaths),
		}
		// customer 模式只采集原始结果，不进行解析
		var fmtRows interface{} = []collect.FormattedRow{}
		if origin != "customer" {
			if r != nil {
				if parsed, err := plugin.Parse(ctxParse, r.Output); err == nil && len(parsed.Rows) > 0 {
					fmtRows = parsed.Rows
				}
			}
		}
		// 错误提示检测：如配置了 error_hints，当输出行以提示前缀开头时标记错误
        detectedErr := ""
        if r != nil && r.Error == "" {
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
		// 若检测到当前命令结束处于特权模式（'#'），停止后续统一提示传播
		if propagateEnableFailed && r != nil && r.Privileged {
			propagateEnableFailed = false
		}

		// 计算错误信息，满足“如果后续任何命令检测到 '#'，停止传播统一提示”
		errVal := func() string {
			if r == nil {
				return ""
			}
			// Cisco 平台：在统一提示传播开启时，仅在当前无显式错误且未停止传播时追加统一提示
			if strings.HasPrefix(platform, "cisco") && enableFailedMsg != "" {
				if r.Error != "" {
					return r.Error
				}
				if detectedErr != "" {
					return detectedErr
				}
				if propagateEnableFailed {
					return fmt.Sprintf("privileged mode not entered (#): %s", enableFailedMsg)
				}
				return ""
			}
			// 非 Cisco 或无 enableFailedMsg：按常规错误输出
			if r.Error != "" {
				return r.Error
			}
			if detectedErr != "" {
				return detectedErr
			}
			return ""
		}()

		view := &CommandResultView{
            Command: displayCmd,
            RawOutput: func() string {
                if r != nil {
                    return stripPagerPrompts(r.Output)
                }
                return ""
            }(),
            FormatOutput: fmtRows,
            Error: errVal,
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
