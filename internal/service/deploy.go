package service

import (
	"context"
	"strings"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// DeployService 提供设备配置快速下发与状态采集能力
type DeployService struct {
	cfg       *config.Config
	collector *CollectorService
	sshPool   *ssh.Pool
}

func NewDeployService(cfg *config.Config, collector *CollectorService) *DeployService {
	return &DeployService{cfg: cfg, collector: collector, sshPool: collector.sshPool}
}

func (s *DeployService) Start(ctx context.Context) error {
	// 输出配置下发服务启动信息与关键 SSH 参数，便于现场定位
	if s == nil || s.cfg == nil {
		logger.Info("Deploy service started")
		return nil
	}
	logger.Info(
		"Deploy service started",
		"ssh_timeout_all", s.cfg.SSH.Timeout,
		"ssh_connect_timeout", s.cfg.SSH.ConnectTimeout,
		"ssh_keep_alive_interval", s.cfg.SSH.KeepAliveInterval,
		"ssh_max_sessions", s.cfg.SSH.MaxSessions,
		"deploy_wait_ms", s.cfg.Deploy.DeployWaitMS,
	)
	return nil
}
func (s *DeployService) Stop() error {
	logger.Info("Deploy service stopped")
	return nil
}

// DeployFastRequest 通用请求
type DeployFastRequest struct {
	TaskID            string         `json:"task_id"`
	TaskName          string         `json:"task_name"`
	RetryFlag         int            `json:"retry_flag"`
	TaskType          string         `json:"task_type"` // exec/dry_run
	TaskTimeout       int            `json:"task_timeout"`
	StatusCheckEnable int            `json:"status_check_enable"` // 1 开启/0 关闭
	Devices           []DeployDevice `json:"devices"`
}

// DeployDevice 单设备参数
type DeployDevice struct {
	DeviceIP        string   `json:"device_ip"`
	DeviceName      string   `json:"device_name"`
	DevicePlatform  string   `json:"device_platform"`
	DevicePort      int      `json:"device_port"`
	CollectProtocol string   `json:"collect_protocol"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password"`
	CliList         []string `json:"cli_list"`
	StatusCheckList []string `json:"status_check_list"`
	ConfigDeploy    string   `json:"config_deploy"`
	DeviceTimeout   *int     `json:"device_timeout,omitempty"`
}

// DeployFastResponse 响应
type DeployFastResponse struct {
	TaskID   string               `json:"task_id"`
	TaskName string               `json:"task_name"`
	Results  []DeployDeviceResult `json:"results"`
	Duration string               `json:"duration"`
}

// 单设备结果
type DeployDeviceResult struct {
	DeviceIP             string            `json:"device_ip"`
	DeviceName           string            `json:"device_name"`
	DevicePlatform       string            `json:"device_platform"`
	DeviceStatusBefore   map[string]string `json:"device_status_before,omitempty"`
	DeviceStatusAfter    map[string]string `json:"device_status_after,omitempty"`
	DeployLogExec        []CommandResult   `json:"deploy_log_exec"`
	DeployLogsAggregated []CommandResult   `json:"deploy_logs_aggregated,omitempty"`
	Error                string            `json:"error,omitempty"`
}

func canonical(cmd string) string {
	s := strings.TrimSpace(cmd)
	if s == "" {
		return s
	}
	// 常见清理：压缩空白与小写化，利于匹配过滤
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

// 读取平台默认配置（设备默认）
func (s *DeployService) getDefaults(platform string) (config.PlatformDefaultsConfig, bool) {
	p := strings.TrimSpace(strings.ToLower(platform))
	if p == "" {
		p = "default"
	}
	// 优先精确匹配
	if s.cfg != nil && s.cfg.Collector.DeviceDefaults != nil {
		if dd, ok := s.cfg.Collector.DeviceDefaults[p]; ok {
			return dd, true
		}
		// 前缀兜底：当 key 为平台前缀时也可匹配（如 huawei、h3c、cisco_ios、linux）
		for key, v := range s.cfg.Collector.DeviceDefaults {
			kk := strings.TrimSpace(strings.ToLower(key))
			if kk == "" {
				continue
			}
			if strings.HasPrefix(p, kk) {
				return v, true
			}
		}
	}
	return config.PlatformDefaultsConfig{}, false
}

// Deploy 执行下发
func (s *DeployService) Deploy(ctx context.Context, req *DeployFastRequest) (*DeployFastResponse, error) {
	start := time.Now()
	resp := &DeployFastResponse{TaskID: req.TaskID, TaskName: req.TaskName, Results: make([]DeployDeviceResult, 0, len(req.Devices))}
	statusEnable := req.StatusCheckEnable

	// 设备循环
	for _, d := range req.Devices {
		r := DeployDeviceResult{DeviceIP: d.DeviceIP, DeviceName: d.DeviceName, DevicePlatform: d.DevicePlatform, DeviceStatusBefore: map[string]string{}, DeviceStatusAfter: map[string]string{}}

		// 计算有效超时：优先设备级，其次任务级，再次全局，最后回退 15s
		effTimeout := req.TaskTimeout
		if effTimeout <= 0 {
			if s.cfg != nil && s.cfg.SSH.Timeout > 0 {
				effTimeout = int(s.cfg.SSH.Timeout.Seconds())
			} else {
				effTimeout = 15
			}
		}
		devTimeout := effTimeout
		if d.DeviceTimeout != nil && *d.DeviceTimeout > 0 {
			devTimeout = *d.DeviceTimeout
		}
		sshTimeout := time.Duration(devTimeout) * time.Second
		// 步骤控制标志与执行间隔
		needsStatus := (statusEnable == 1) && (len(d.StatusCheckList) > 0) && (s.collector != nil)
		doDeploy := strings.EqualFold(strings.TrimSpace(req.TaskType), "exec")
		wait := s.cfg.Deploy.DeployWaitMS
		if wait <= 0 {
			wait = 2000
		}

		// 采集前状态：改为调用 CollectorService
		if needsStatus {
			cTimeout := req.TaskTimeout
			if cTimeout <= 0 {
				// 使用全局 ssh.timeout.timeout_all 作为默认值（秒），回退 15s
				if s.cfg != nil && s.cfg.SSH.Timeout > 0 {
					cTimeout = int(s.cfg.SSH.Timeout.Seconds())
				} else {
					cTimeout = 15
				}
			}
			rf := req.RetryFlag
			creq := &CollectRequest{
				TaskID:          req.TaskID + "-pre-" + d.DeviceIP,
				TaskName:        req.TaskName,
				CollectOrigin:   "customer",
				DeviceIP:        d.DeviceIP,
				DeviceName:      d.DeviceName,
				DevicePlatform:  d.DevicePlatform,
				CollectProtocol: "ssh",
				Port:            d.DevicePort,
				UserName:        d.UserName,
				Password:        d.Password,
				EnablePassword:  d.EnablePassword,
				CliList:         d.StatusCheckList,
				RetryFlag:       &rf,
				TaskTimeout:     &cTimeout,
				DeviceTimeout:   d.DeviceTimeout,
				Metadata:        map[string]interface{}{"collect_mode": "customer"},
			}
			if cresp, err := s.collector.ExecuteTask(ctx, creq); err == nil && cresp != nil {
				for _, v := range cresp.Results {
					if v == nil {
						continue
					}
					cmd := strings.TrimSpace(v.Command)
					r.DeviceStatusBefore[cmd] = v.RawOutput
				}
			}
			// 步骤间隔：采集前与后续步骤之间
			time.Sleep(time.Duration(wait) * time.Millisecond)
		}

		// 配置下发阶段：仅当 task_type=exec 执行
		if doDeploy {
			// 建立设备连接并准备交互选项
			if s.sshPool == nil {
				r.Error = "ssh pool not initialized"
				resp.Results = append(resp.Results, r)
				continue
			}
			info := &ssh.ConnectionInfo{
				Host:     d.DeviceIP,
				Port:     d.DevicePort,
				Username: d.UserName,
				Password: d.Password,
			}
			connCtx, cancel := context.WithTimeout(ctx, sshTimeout)
			cli, err := s.sshPool.GetConnection(connCtx, info)
			cancel()
			if err != nil {
				r.Error = "connect failed: " + err.Error()
				resp.Results = append(resp.Results, r)
				continue
			}
			// 平台交互默认与节奏
			p := s.getPlatformInteract(d.DevicePlatform)
			cmdInterval := p.CommandIntervalMS
			if cmdInterval <= 0 {
				cmdInterval = 120
			}
			opts := &ssh.InteractiveOptions{
				EnablePassword:           strings.TrimSpace(d.EnablePassword),
				LoginPassword:            strings.TrimSpace(d.Password),
				EnableCLI:                p.EnableCLI,
				EnableExpectOutput:       p.EnableExceptOutput,
				ExitCommands:             []string{"exit"},
				CommandIntervalMS:        cmdInterval,
				AutoInteractions:         p.AutoInteractions,
				SkipDelayedEcho:          p.SkipDelayedEcho,
				PerCommandTimeoutSec:     p.CommandTimeoutSec,
				QuietAfterMS:             p.QuietAfterMS,
				QuietPollIntervalMS:      p.QuietPollIntervalMS,
				EnablePasswordFallbackMS: p.EnablePasswordFallbackMS,
				PromptInducerIntervalMS:  p.PromptInducerIntervalMS,
				PromptInducerMaxCount:    p.PromptInducerMaxCount,
				ExitPauseMS:              p.ExitPauseMS,
				// 新增：用于精确提示符判定
				DeviceName: strings.TrimSpace(d.DeviceName),
				// 新增：设备平台用于区分不同平台的处理逻辑
				DevicePlatform: strings.TrimSpace(d.DevicePlatform),
				PromptSuffixes: p.PromptSuffixes,
			}
			// 用户下发序列（预命令 + 进入配置模式 + 用户命令 + 退出配置模式）
			pre := s.getPreCommands(d.DevicePlatform)
			configEnter := s.getConfigModeCmds(d.DevicePlatform)
			exitCmd := s.getConfigExitCmd(d.DevicePlatform)
			// 将 config_deploy 兼容为用户命令列表（当 cli_list 为空时）
			userCmds := make([]string, 0, len(d.CliList))
			for _, c := range d.CliList {
				if t := strings.TrimSpace(c); t != "" {
					userCmds = append(userCmds, t)
				}
			}
			if len(userCmds) == 0 && strings.TrimSpace(d.ConfigDeploy) != "" {
				raw := strings.ReplaceAll(d.ConfigDeploy, "\r\n", "\n")
				for _, ln := range strings.Split(raw, "\n") {
					if t := strings.TrimSpace(ln); t != "" {
						userCmds = append(userCmds, t)
					}
				}
			}
			// 保留原始用户命令（不进行规范化/映射）
			// 条件退出配置模式：在 SSH 交互中根据提示符判定是否需要执行退出
			opts.ConfigExitCLI = exitCmd
			opts.ConfigExitConditional = true
			deploySeq := append([]string{}, pre...)
			deploySeq = append(deploySeq, configEnter...)
			deploySeq = append(deploySeq, userCmds...)
			// 保护：若用户已包含退出命令（如 end/quit），则不再附加平台退出命令
			userHasExit := false
			if strings.TrimSpace(exitCmd) != "" {
				ce := canonical(exitCmd)
				for _, u := range userCmds {
					if canonical(u) == ce {
						userHasExit = true
						break
					}
				}
			}
			if !userHasExit && strings.TrimSpace(exitCmd) != "" {
				deploySeq = append(deploySeq, exitCmd)
			}

			// 执行详细日志（逐条）
			sessionLogs := s.runCommandsDetailed(ctx, cli, deploySeq, p.PromptSuffixes, opts)
			// 释放连接到全局池（每台设备完成后立即释放，避免 defer 堆积）
			s.sshPool.ReleaseConnection(info)

			// 仅保留用户命令对应的回显作为 deploy_log_exec
			include := map[string]struct{}{}
			for _, c := range userCmds {
				k := canonical(c)
				if k != "" {
					include[k] = struct{}{}
				}
			}
			filteredLogs := make([]CommandResult, 0, len(userCmds))
			for _, lr := range sessionLogs {
				key := canonical(lr.Command)
				if _, ok := include[key]; ok {
					filteredLogs = append(filteredLogs, lr)
				}
			}
			// 新增：根据平台错误提示调整 ExitCode 与错误字段，便于定位下发失败
			if len(filteredLogs) > 0 {
				// 读取平台错误提示集合
				pi := s.getPlatformInteract(d.DevicePlatform)
				for i := range filteredLogs {
					outLower := strings.ToLower(filteredLogs[i].Output)
					// 命中任一错误提示则认为下发失败，标记 ExitCode=-1
					for _, hint := range pi.ErrorHints {
						h := strings.ToLower(strings.TrimSpace(hint))
						if h == "" {
							continue
						}
						if strings.Contains(outLower, h) {
							if filteredLogs[i].ExitCode == 0 {
								filteredLogs[i].ExitCode = -1
							}
							if strings.TrimSpace(filteredLogs[i].Error) == "" {
								filteredLogs[i].Error = "deployment command error detected"
							}
							break
						}
					}
				}
			}
			r.DeployLogExec = filteredLogs
			// 组装聚合输出（模拟粘贴式整体回显）
			agg := s.aggregateDeployLogs(userCmds, filteredLogs)
			r.DeployLogsAggregated = []CommandResult{agg}
		} else {
			// 跳过真实下发：构造空执行日志与聚合
			filteredLogs := make([]CommandResult, 0)
			r.DeployLogExec = filteredLogs
			// 使用 config_deploy 或 cli_list 构造聚合命令行，便于前端显示
			userCmds := make([]string, 0, len(d.CliList))
			for _, c := range d.CliList {
				if t := strings.TrimSpace(c); t != "" {
					userCmds = append(userCmds, t)
				}
			}
			if len(userCmds) == 0 && strings.TrimSpace(d.ConfigDeploy) != "" {
				raw := strings.ReplaceAll(d.ConfigDeploy, "\r\n", "\n")
				for _, ln := range strings.Split(raw, "\n") {
					if t := strings.TrimSpace(ln); t != "" {
						userCmds = append(userCmds, t)
					}
				}
			}
			agg := s.aggregateDeployLogs(userCmds, filteredLogs)
			r.DeployLogsAggregated = []CommandResult{agg}
		}

		// 步骤间隔：配置下发与后续状态采集之间（如果需要）
		if needsStatus {
			time.Sleep(time.Duration(wait) * time.Millisecond)
		}

		// 下发后的设备信息采集（可选）
		if needsStatus {
			cTimeout := req.TaskTimeout
			if cTimeout <= 0 {
				if s.cfg != nil && s.cfg.SSH.Timeout > 0 {
					cTimeout = int(s.cfg.SSH.Timeout.Seconds())
				} else {
					cTimeout = 15
				}
			}
			rf := req.RetryFlag
			creq := &CollectRequest{
				TaskID:          req.TaskID + "-post-" + d.DeviceIP,
				TaskName:        req.TaskName,
				CollectOrigin:   "customer",
				DeviceIP:        d.DeviceIP,
				DeviceName:      d.DeviceName,
				DevicePlatform:  d.DevicePlatform,
				CollectProtocol: "ssh",
				Port:            d.DevicePort,
				UserName:        d.UserName,
				Password:        d.Password,
				EnablePassword:  d.EnablePassword,
				CliList:         d.StatusCheckList,
				RetryFlag:       &rf,
				TaskTimeout:     &cTimeout,
				DeviceTimeout:   d.DeviceTimeout,
				Metadata:        map[string]interface{}{"collect_mode": "customer"},
			}
			if cresp, err := s.collector.ExecuteTask(ctx, creq); err == nil && cresp != nil {
				for _, v := range cresp.Results {
					if v == nil {
						continue
					}
					cmd := strings.TrimSpace(v.Command)
					r.DeviceStatusAfter[cmd] = v.RawOutput
				}
			}
		}

		resp.Results = append(resp.Results, r)
	}
	resp.Duration = time.Since(start).String()
	return resp, nil
}

// getPlatformInteract 读取平台交互默认，避免与其他服务深耦合，这里做最小复制
type platformInteract struct {
	PromptSuffixes           []string
	AutoInteractions         []ssh.AutoInteraction
	SkipDelayedEcho          bool
	EnableCLI                string
	EnableExceptOutput       string
	ErrorHints               []string
	CommandIntervalMS        int
	CommandTimeoutSec        int
	QuietAfterMS             int
	QuietPollIntervalMS      int
	EnablePasswordFallbackMS int
	PromptInducerIntervalMS  int
	PromptInducerMaxCount    int
	ExitPauseMS              int
}

func (s *DeployService) getPlatformInteract(platform string) platformInteract {
	dd, ok := s.getDefaults(platform)
	p := platformInteract{}
	if !ok {
		return p
	}
	p.PromptSuffixes = append([]string{}, dd.PromptSuffixes...)
	// 转换配置中的自动交互项到 SSH 类型
	p.AutoInteractions = make([]ssh.AutoInteraction, 0, len(dd.Interact.AutoInteractions))
	for _, ai := range dd.Interact.AutoInteractions {
		e := strings.TrimSpace(ai.ExpectOutput)
		s := strings.TrimSpace(ai.AutoSend)
		if e == "" || s == "" {
			continue
		}
		p.AutoInteractions = append(p.AutoInteractions, ssh.AutoInteraction{ExpectOutput: e, AutoSend: s})
	}
	p.SkipDelayedEcho = dd.SkipDelayedEcho
	p.EnableCLI = dd.EnableCLI
	p.EnableExceptOutput = dd.EnableExceptOutput
	p.ErrorHints = append([]string{}, dd.Interact.ErrorHints...)
	// 交互节奏回退
	if dd.Timeout.Interact.CommandIntervalMS > 0 {
		p.CommandIntervalMS = dd.Timeout.Interact.CommandIntervalMS
	} else {
		p.CommandIntervalMS = 120
	}
	if dd.Timeout.Interact.CommandTimeoutSec > 0 {
		p.CommandTimeoutSec = dd.Timeout.Interact.CommandTimeoutSec
	} else {
		p.CommandTimeoutSec = 30
	}
	if dd.Timeout.Interact.QuietAfterMS > 0 {
		p.QuietAfterMS = dd.Timeout.Interact.QuietAfterMS
	} else {
		p.QuietAfterMS = 800
	}
	if dd.Timeout.Interact.QuietPollIntervalMS > 0 {
		p.QuietPollIntervalMS = dd.Timeout.Interact.QuietPollIntervalMS
	} else {
		p.QuietPollIntervalMS = 250
	}
	if dd.Timeout.Interact.EnablePasswordFallbackMS > 0 {
		p.EnablePasswordFallbackMS = dd.Timeout.Interact.EnablePasswordFallbackMS
	} else {
		p.EnablePasswordFallbackMS = 1500
	}
	if dd.Timeout.Interact.PromptInducerIntervalMS > 0 {
		p.PromptInducerIntervalMS = dd.Timeout.Interact.PromptInducerIntervalMS
	} else {
		p.PromptInducerIntervalMS = 1000
	}
	if dd.Timeout.Interact.PromptInducerMaxCount > 0 {
		p.PromptInducerMaxCount = dd.Timeout.Interact.PromptInducerMaxCount
	} else {
		p.PromptInducerMaxCount = 12
	}
	if dd.Timeout.Interact.ExitPauseMS > 0 {
		p.ExitPauseMS = dd.Timeout.Interact.ExitPauseMS
	} else {
		p.ExitPauseMS = 150
	}
	return p
}

// 获取平台预命令：如enable、关闭分页
func (s *DeployService) getPreCommands(platform string) []string {
	cmds := []string{}
	dd, ok := s.getDefaults(platform)
	if !ok {
		return cmds
	}

	// 如果平台需要enable，先添加enable命令
	if dd.EnableRequired && strings.TrimSpace(dd.EnableCLI) != "" {
		cmds = append(cmds, strings.TrimSpace(dd.EnableCLI))
	}

	// 添加关闭分页命令
	for _, c := range dd.DisablePagingCmds {
		t := strings.TrimSpace(c)
		if t == "" {
			continue
		}
		cmds = append(cmds, t)
	}
	return cmds
}

// 新增：获取平台配置模式命令列表
func (s *DeployService) getConfigModeCmds(platform string) []string {
	cmds := []string{}
	dd, ok := s.getDefaults(platform)
	if !ok {
		return cmds
	}
	for _, c := range dd.ConfigModeCLIs {
		t := strings.TrimSpace(c)
		if t != "" {
			cmds = append(cmds, t)
		}
	}
	return cmds
}

// 获取平台退出配置模式命令
func (s *DeployService) getConfigExitCmd(platform string) string {
	dd, ok := s.getDefaults(platform)
	if !ok {
		return ""
	}
	return strings.TrimSpace(dd.ConfigExitCLI)
}

// runCommandsDetailed 返回详细执行日志（逐条）
func (s *DeployService) runCommandsDetailed(ctx context.Context, cli *ssh.Client, cmds []string, promptSuffixes []string, opts *ssh.InteractiveOptions) []CommandResult {
	logs := make([]CommandResult, 0, len(cmds))
	if len(cmds) == 0 {
		return logs
	}
	results, err := cli.ExecuteInteractiveCommands(ctx, cmds, promptSuffixes, opts)
	if err != nil {
		// 即使出错（如上下文超时），客户端也会返回部分结果；继续写入
		for _, cr := range results {
			if cr == nil {
				continue
			}
			logs = append(logs, CommandResult{
				Command:  strings.TrimSpace(cr.Command),
				Output:   cr.Output,
				Error:    cr.Error,
				Elapsed:  cr.Duration.String(),
				ExitCode: cr.ExitCode,
			})
		}
		// 附加一条总错误信息，便于定位
		logs = append(logs, CommandResult{Command: "__deploy__", Output: "", Error: err.Error(), Elapsed: "", ExitCode: -1})
		return logs
	}
	for _, cr := range results {
		if cr == nil {
			continue
		}
		logs = append(logs, CommandResult{
			Command:  strings.TrimSpace(cr.Command),
			Output:   cr.Output,
			Error:    cr.Error,
			Elapsed:  cr.Duration.String(),
			ExitCode: cr.ExitCode,
		})
	}
	return logs
}

// 新增：根据逐条日志聚合输出（不重复执行命令）
// 按照 command + output 交替的格式进行聚合
func (s *DeployService) aggregateDeployLogs(cmds []string, logs []CommandResult) CommandResult {
	agg := CommandResult{Command: "", Output: "", Error: "", Elapsed: "", ExitCode: 0}
	if len(cmds) > 0 {
		agg.Command = strings.Join(cmds, "\n") + "\n"
	}
	var dur time.Duration
	var outSB strings.Builder
	var errSB strings.Builder
	
	for _, cr := range logs {
		// 跳过内部错误记录项
		if strings.TrimSpace(cr.Command) == "__deploy__" {
			if strings.TrimSpace(cr.Error) != "" && agg.Error == "" {
				agg.Error = strings.TrimSpace(cr.Error)
			}
			continue
		}
		
		// 按照 command + output 的格式进行聚合
		// line1: command
		// line2: command-output
		if strings.TrimSpace(cr.Command) != "" {
			outSB.WriteString(strings.TrimSpace(cr.Command))
			outSB.WriteString("\n")
		}
		
		if strings.TrimSpace(cr.Output) != "" {
			outSB.WriteString(cr.Output)
			if !strings.HasSuffix(cr.Output, "\n") {
				outSB.WriteString("\n")
			}
		}
		
		// 收集错误信息
		if strings.TrimSpace(cr.Error) != "" {
			errSB.WriteString(cr.Error)
			if !strings.HasSuffix(cr.Error, "\n") {
				errSB.WriteString("\n")
			}
		}
		
		// 累计执行时间
		if strings.TrimSpace(cr.Elapsed) != "" {
			if d, e := time.ParseDuration(cr.Elapsed); e == nil {
				dur += d
			}
		}
	}
	
	agg.Output = outSB.String()
	if agg.Error == "" && errSB.Len() > 0 {
		agg.Error = strings.TrimSuffix(errSB.String(), "\n")
	}
	if dur > 0 {
		agg.Elapsed = dur.String()
	}
	return agg
}

// 兼容旧接口：保留 ExecuteFast，内部转发到 Deploy
func (s *DeployService) ExecuteFast(ctx context.Context, req *DeployFastRequest) (*DeployFastResponse, error) {
	return s.Deploy(ctx, req)
}

// CommandResult 记录每条命令执行的输出
type CommandResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error"`
	Elapsed  string `json:"elapsed"`
	ExitCode int    `json:"exit_code"`
}
