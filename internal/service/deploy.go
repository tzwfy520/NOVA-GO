package service

import (
	"context"
	"strings"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// DeployService 提供设备配置快速下发与状态采集能力
type DeployService struct {
	cfg       *config.Config
	collector *CollectorService
}

func NewDeployService(cfg *config.Config, collector *CollectorService) *DeployService {
	return &DeployService{cfg: cfg, collector: collector}
}

func (s *DeployService) Start(ctx context.Context) error { return nil }
func (s *DeployService) Stop() error                     { return nil }

// DeployFastRequest 通用请求
type DeployFastRequest struct {
	TaskID            string         `json:"task_id"`
	TaskName          string         `json:"task_name"`
	RetryFlag         int            `json:"retry_flag"`
	TaskType          string         `json:"task_type"` // exec/dry_run
	Timeout           int            `json:"timeout"`
	StatusCheckEnable int            `json:"status_check_enable"` // 1 开启/0 关闭
	Devices           []DeployDevice `json:"devices"`
}

// DeployDevice 设备参数
type DeployDevice struct {
	DeviceIP        string   `json:"device_ip"`
	DevicePort      int      `json:"device_port"`
	DeviceName      string   `json:"device_name"`
	DevicePlatform  string   `json:"device_platform"`
	CollectProtocol string   `json:"collect_protocol"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password"`
	StatusCheckList []string `json:"status_check_list"`
	ConfigDeploy    string   `json:"config_deploy"`
}

// DeployFastResponse 返回每台设备的状态与下发结果
type DeployFastResponse struct {
	TaskID   string               `json:"task_id"`
	TaskName string               `json:"task_name"`
	Results  []DeployDeviceResult `json:"results"`
	Duration string               `json:"duration"`
}

// DeployDeviceResult 单设备结果
type DeployDeviceResult struct {
	DeviceIP           string            `json:"device_ip"`
	DeviceName         string            `json:"device_name"`
	DevicePlatform     string            `json:"device_platform"`
	DeviceStatusBefore map[string]string `json:"device_status_before,omitempty"`
	DeviceStatusAfter  map[string]string `json:"device_status_after,omitempty"`
	DeployLogExec      []CommandResult   `json:"deploy_log_exec"`
	DeployLogsAggregated []CommandResult `json:"deploy_logs_aggregated,omitempty"`
	Error              string            `json:"error,omitempty"`
}

// CommandResult 记录每条命令执行的输出
type CommandResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error"`
	Elapsed  string `json:"elapsed"`
	ExitCode int    `json:"exit_code"`
}

// 规范化字符串：trim + toLower
func canonical(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// 平台默认检索（统一入口）
func (s *DeployService) getDefaults(platform string) (config.PlatformDefaultsConfig, bool) {
	if s == nil || s.cfg == nil || s.cfg.Collector.DeviceDefaults == nil {
		return config.PlatformDefaultsConfig{}, false
	}
	p := strings.TrimSpace(platform)
	if p == "" { p = "default" }
	if def, ok := s.cfg.Collector.DeviceDefaults[p]; ok { return def, true }
	if def, ok := s.cfg.Collector.DeviceDefaults["default"]; ok { return def, true }
	return config.PlatformDefaultsConfig{}, false
}

func (s *DeployService) ExecuteFast(ctx context.Context, req *DeployFastRequest) (*DeployFastResponse, error) {
	start := time.Now()
	resp := &DeployFastResponse{TaskID: "", TaskName: "", Results: []DeployDeviceResult{}, Duration: ""}
	if req != nil {
		resp.TaskID = req.TaskID
		resp.TaskName = req.TaskName
	}

	if req == nil || len(req.Devices) == 0 {
		return resp, nil
	}

	statusEnable := 0
	if req.StatusCheckEnable == 1 {
		statusEnable = 1
	}

	for _, d := range req.Devices {
		if d.DevicePort <= 0 { d.DevicePort = 22 }
		if strings.TrimSpace(d.CollectProtocol) == "" { d.CollectProtocol = "ssh" }

		connCtx, cancel := context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		if req.Timeout <= 0 { connCtx, cancel = context.WithTimeout(ctx, 15*time.Second) }
		defer cancel()

		r := DeployDeviceResult{
			DeviceIP:           d.DeviceIP,
			DeviceName:         d.DeviceName,
			DevicePlatform:     d.DevicePlatform,
			DeviceStatusBefore: map[string]string{},
			DeviceStatusAfter:  map[string]string{},
			DeployLogExec:      []CommandResult{},
			DeployLogsAggregated: []CommandResult{},
			Error:              "",
		}

		sshTimeout := time.Duration(req.Timeout) * time.Second
		if req.Timeout <= 0 {
		    sshTimeout = s.cfg.SSH.Timeout
		}
		connectTimeout := s.cfg.SSH.ConnectTimeout
		keepAlive := s.cfg.SSH.KeepAliveInterval
		maxSessions := s.cfg.SSH.MaxSessions
		if maxSessions <= 0 { maxSessions = 1 }
		
		cli := ssh.NewClient(&ssh.Config{
		    Timeout:        sshTimeout,
		    ConnectTimeout: connectTimeout,
		    KeepAlive:      keepAlive,
		    MaxSessions:    maxSessions,
		})
		if err := cli.Connect(connCtx, &ssh.ConnectionInfo{
			Host:     d.DeviceIP,
			Port:     d.DevicePort,
			Username: d.UserName,
			Password: d.Password,
		}); err != nil {
			r.Error = "connect failed: " + err.Error()
			resp.Results = append(resp.Results, r)
			continue
		}

		// 平台交互默认
		p := s.getPlatformInteract(d.DevicePlatform)
		opts := &ssh.InteractiveOptions{
			EnablePassword:     strings.TrimSpace(d.EnablePassword),
			LoginPassword:      strings.TrimSpace(d.Password),
			EnableCLI:          p.EnableCLI,
			EnableExpectOutput: p.EnableExceptOutput,
			ExitCommands:       []string{"exit"},
			CommandIntervalMS:  120,
			AutoInteractions:   p.AutoInteractions,
			SkipDelayedEcho:    p.SkipDelayedEcho,
		}

		// 采集前状态：改为调用 CollectorService
		if statusEnable == 1 && s.collector != nil {
			cTimeout := req.Timeout
			if cTimeout <= 0 {
				cTimeout = 15
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
				Timeout:         &cTimeout,
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
			} else if err != nil {
				// 记录错误但不中断下发流程
				r.DeviceStatusBefore["__error__"] = err.Error()
			}
			// 前采集完成后，按配置等待 deploy_wait_ms
			wait := s.cfg.Deploy.DeployWaitMS
			if wait <= 0 { wait = 2000 }
			time.Sleep(time.Duration(wait) * time.Millisecond)
		}

		// 部署前平台预命令（取消分页、必要时 enable），避免与用户命令重复注入
		deploySeq := s.buildDeploySequence(d.DevicePlatform, d.ConfigDeploy)
		preCmds := s.getPreCommands(d.DevicePlatform, deploySeq)
		// 将 enable/关闭分页 + 进入配置模式 + 用户命令合并到同一交互会话，避免跨会话丢失特权状态
		cfgModeCmds := s.getConfigModeCmds(d.DevicePlatform)
		combined := make([]string, 0, len(preCmds)+len(cfgModeCmds)+len(deploySeq))
		if len(preCmds) > 0 { combined = append(combined, preCmds...) }
		if len(cfgModeCmds) > 0 { combined = append(combined, cfgModeCmds...) }
		if len(deploySeq) > 0 { combined = append(combined, deploySeq...) }

		// 下发配置（支持 dry_run）
		if strings.TrimSpace(req.TaskType) != "dry_run" {
			// 在同一交互会话中执行完整序列
			sessionLogs := s.runCommandsDetailed(connCtx, cli, combined, p.PromptSuffixes, opts)
			// 若提权命令未进入特权提示符（ExitCode=-2），标记错误
			ecli := strings.TrimSpace(p.EnableCLI)
			if ecli == "" { ecli = "enable" }
			for _, lr := range sessionLogs {
				if strings.EqualFold(strings.TrimSpace(lr.Command), ecli) {
					if lr.ExitCode == -2 {
						r.Error = "privileged mode required but enable failed: exit_code=-2"
						break
					}
					if strings.TrimSpace(lr.Error) != "" {
						r.Error = "privileged mode required but enable failed: " + strings.TrimSpace(lr.Error)
						break
					}
				}
			}
			// 仅保留用户命令对应的回显作为 deploy_log_exec（即使 enable 失败也保留用户命令输出，便于诊断）
			include := map[string]struct{}{}
			for _, c := range deploySeq {
				k := canonical(c)
				if k != "" { include[k] = struct{}{} }
			}
			filteredLogs := make([]CommandResult, 0, len(deploySeq))
			for _, lr := range sessionLogs {
				key := canonical(lr.Command)
				if _, ok := include[key]; ok {
					filteredLogs = append(filteredLogs, lr)
				}
			}
			r.DeployLogExec = filteredLogs
			// 组装聚合输出（模拟粘贴式整体回显）
			agg := s.aggregateDeployLogs(deploySeq, filteredLogs)
			r.DeployLogsAggregated = []CommandResult{agg}
			// 下发完成后，按配置等待 deploy_wait_ms 再进行后续状态采集
			wait := s.cfg.Deploy.DeployWaitMS
			if wait <= 0 { wait = 2000 }
			time.Sleep(time.Duration(wait) * time.Millisecond)
		}

		// 采集后状态：改为调用 CollectorService
		if statusEnable == 1 && s.collector != nil {
			cTimeout := req.Timeout
			if cTimeout <= 0 {
				cTimeout = 15
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
				Timeout:         &cTimeout,
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
			} else if err != nil {
				r.DeviceStatusAfter["__error__"] = err.Error()
			}
		}

		cli.Close()
		resp.Results = append(resp.Results, r)
	}

	resp.Duration = time.Since(start).String()
	return resp, nil
}

// getPlatformInteract 读取平台交互默认，避免与其他服务深耦合，这里做最小复制
type platformInteract struct {
	PromptSuffixes     []string
	AutoInteractions   []ssh.AutoInteraction
	SkipDelayedEcho    bool
	EnableCLI          string
	EnableExceptOutput string
	ErrorHints         []string
}

func (s *DeployService) getPlatformInteract(platform string) platformInteract {
	// 默认值
	out := platformInteract{
		PromptSuffixes:     []string{"#", ">"},
		SkipDelayedEcho:    true,
		EnableCLI:          "enable",
		EnableExceptOutput: "password",
		AutoInteractions: []ssh.AutoInteraction{
			{ExpectOutput: "--More--", AutoSend: " "},
		},
		ErrorHints: []string{"invalid", "unknown command", "error"},
	}
	if s == nil || s.cfg == nil {
		return out
	}
	p := strings.TrimSpace(platform)
	if p == "" {
		p = "default"
	}
	if def, ok := s.cfg.Collector.DeviceDefaults[p]; ok {
		// PromptSuffixes
		if len(def.PromptSuffixes) > 0 {
			out.PromptSuffixes = def.PromptSuffixes
		}
		// AutoInteractions
		if len(def.AutoInteractions) > 0 {
			out.AutoInteractions = make([]ssh.AutoInteraction, 0, len(def.AutoInteractions))
			for _, ai := range def.AutoInteractions {
				if ai.ExpectOutput == "" || ai.AutoSend == "" {
					continue
				}
				out.AutoInteractions = append(out.AutoInteractions, ssh.AutoInteraction{
					ExpectOutput: ai.ExpectOutput,
					AutoSend:     ai.AutoSend,
				})
			}
		}
		// SkipDelayedEcho
		out.SkipDelayedEcho = def.SkipDelayedEcho
		// EnableCLI / except_output
		if strings.TrimSpace(def.EnableCLI) != "" {
			out.EnableCLI = def.EnableCLI
		}
		if strings.TrimSpace(def.EnableExceptOutput) != "" {
			out.EnableExceptOutput = def.EnableExceptOutput
		}
		// ErrorHints（优先 interact 内嵌，其次兼容旧字段）
		if len(def.Interact.ErrorHints) > 0 {
			out.ErrorHints = def.Interact.ErrorHints
		} else if len(def.ErrorHints) > 0 {
			out.ErrorHints = def.ErrorHints
		}
	} else if def, ok := s.cfg.Collector.DeviceDefaults["default"]; ok {
		if len(def.PromptSuffixes) > 0 {
			out.PromptSuffixes = def.PromptSuffixes
		}
		out.SkipDelayedEcho = def.SkipDelayedEcho
		if strings.TrimSpace(def.EnableCLI) != "" {
			out.EnableCLI = def.EnableCLI
		}
		if strings.TrimSpace(def.EnableExceptOutput) != "" {
			out.EnableExceptOutput = def.EnableExceptOutput
		}
		if len(def.AutoInteractions) > 0 {
			out.AutoInteractions = make([]ssh.AutoInteraction, 0, len(def.AutoInteractions))
			for _, ai := range def.AutoInteractions {
				if ai.ExpectOutput == "" || ai.AutoSend == "" {
					continue
				}
				out.AutoInteractions = append(out.AutoInteractions, ssh.AutoInteraction{ExpectOutput: ai.ExpectOutput, AutoSend: ai.AutoSend})
			}
		}
		if len(def.Interact.ErrorHints) > 0 {
			out.ErrorHints = def.Interact.ErrorHints
		} else if len(def.ErrorHints) > 0 {
			out.ErrorHints = def.ErrorHints
		}
	}
	return out
}

// buildDeploySequence 构建下发序列，仅返回用户提供的命令（移除平台层的配置模式命令）
func (s *DeployService) buildDeploySequence(platform string, cfgText string) []string {
	lines := []string{}
	for _, ln := range strings.Split(cfgText, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		lines = append(lines, t)
	}
	// 过滤掉与平台 config_mode_clis 重复的命令（避免在部署序列中再次出现）
	dd, ok := s.getDefaults(platform)
	if ok && len(dd.ConfigModeCLIs) > 0 {
		idx := map[string]struct{}{}
		for _, c := range dd.ConfigModeCLIs {
			idx[canonical(c)] = struct{}{}
		}
		filtered := make([]string, 0, len(lines))
		for _, l := range lines {
			if _, exists := idx[canonical(l)]; exists {
				continue
			}
			filtered = append(filtered, l)
		}
		lines = filtered
	}
	return lines
}



// 新增：尝试进入特权模式（enable/sudo），失败则返回 false

// 新增：部署会话预命令生成（enable + 取消分页，避免与用户重复）
func (s *DeployService) getPreCommands(platform string, user []string) []string {
	out := make([]string, 0, 4)
	// 平台默认
	dd, ok := s.getDefaults(platform)
	// 用户命令索引
	has := func(cmd string) bool {
		key := canonical(cmd)
		for _, c := range user {
			if canonical(c) == key { return true }
		}
		return false
	}
	if ok && dd.EnableRequired {
		ecmd := strings.TrimSpace(dd.EnableCLI)
		if ecmd == "" { ecmd = "enable" }
		if !has(ecmd) { out = append(out, ecmd) }
	}
	for _, pc := range dd.DisablePagingCmds {
		c := strings.TrimSpace(pc)
		if c == "" { continue }
		if !has(c) { out = append(out, c) }
	}
	return out
}

// 新增：获取平台配置模式命令列表
func (s *DeployService) getConfigModeCmds(platform string) []string {
	cmds := []string{}
	dd, ok := s.getDefaults(platform)
	if !ok { return cmds }
	for _, c := range dd.ConfigModeCLIs {
		t := strings.TrimSpace(c)
		if t != "" { cmds = append(cmds, t) }
	}
	return cmds
}

// 新增：按序尝试进入配置模式（匹配到一个成功后停止）

// runCommandsDetailed 返回详细执行日志（逐条）
func (s *DeployService) runCommandsDetailed(ctx context.Context, cli *ssh.Client, cmds []string, promptSuffixes []string, opts *ssh.InteractiveOptions) []CommandResult {
    logs := make([]CommandResult, 0, len(cmds))
    if len(cmds) == 0 { return logs }
    results, err := cli.ExecuteInteractiveCommands(ctx, cmds, promptSuffixes, opts)
    if err != nil {
        // 即使出错（如上下文超时），客户端也会返回部分结果；继续写入
        for _, cr := range results {
            if cr == nil { continue }
            logs = append(logs, CommandResult{
                Command: strings.TrimSpace(cr.Command),
                Output:  cr.Output,
                Error:   cr.Error,
                Elapsed: cr.Duration.String(),
                ExitCode: cr.ExitCode,
            })
        }
        // 附加一条总错误信息，便于定位
        logs = append(logs, CommandResult{Command: "__deploy__", Output: "", Error: err.Error(), Elapsed: "", ExitCode: -1})
        return logs
    }
    for _, cr := range results {
        if cr == nil { continue }
        logs = append(logs, CommandResult{
            Command: strings.TrimSpace(cr.Command),
            Output:  cr.Output,
            Error:   cr.Error,
            Elapsed: cr.Duration.String(),
            ExitCode: cr.ExitCode,
        })
    }
    return logs
}

// 新增：根据逐条日志聚合输出（不重复执行命令）
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
        if strings.TrimSpace(cr.Output) != "" {
            outSB.WriteString(cr.Output)
            if !strings.HasSuffix(cr.Output, "\n") {
                outSB.WriteString("\n")
            }
        }
        if strings.TrimSpace(cr.Error) != "" {
            errSB.WriteString(cr.Error)
            if !strings.HasSuffix(cr.Error, "\n") {
                errSB.WriteString("\n")
            }
        }
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
