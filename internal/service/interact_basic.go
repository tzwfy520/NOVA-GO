package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// ExecRequest 执行器输入参数（设备连接信息）
type ExecRequest struct {
	DeviceIP        string
	Port            int
	DeviceName      string
	DevicePlatform  string
	CollectProtocol string // ssh
	UserName        string
	Password        string
	EnablePassword  string
	TaskTimeoutSec   int
	DeviceTimeoutSec int
}

// InteractBasic 统一的设备基础交互入口：
// - 内联执行逻辑：交互优先、失败回退非交互（原 ExecAdapter.Execute）
// - 标准化输出：去除内部预命令（enable/关闭分页），应用统一的行过滤
// - 面向服务层暴露统一交互入口，避免重复注入与过滤
type InteractBasic struct {
	cfg  *config.Config
	pool *ssh.Pool
}

func NewInteractBasic(cfg *config.Config, pool *ssh.Pool) *InteractBasic {
	return &InteractBasic{cfg: cfg, pool: pool}
}

// Execute 执行用户命令：
// 1) 通过适配器执行（交互优先，必要时回退非交互）
// 2) 移除内部预命令对应的结果（enable、关闭分页）
// 3) 应用统一的输出行过滤（collector.output_filter）
func (b *InteractBasic) Execute(ctx context.Context, req *ExecRequest, userCommands []string) ([]*ssh.CommandResult, error) {
	// 协议校验与默认
	if strings.TrimSpace(req.CollectProtocol) == "" {
		req.CollectProtocol = "ssh"
	}
	if strings.ToLower(req.CollectProtocol) != "ssh" {
		return nil, fmt.Errorf("unsupported protocol: %s", req.CollectProtocol)
	}

	// 端口校正
	port := req.Port
	if port < 1 || port > 65535 {
		port = 22
	}

	conn := &ssh.ConnectionInfo{
		Host:     req.DeviceIP,
		Port:     port,
		Username: req.UserName,
		Password: req.Password,
	}

	// 任务超时控制（用于整个执行窗口）
	effTaskTimeout := req.TaskTimeoutSec
	if effTaskTimeout <= 0 {
		effTaskTimeout = 30
	}
	execCtx, cancelExec := context.WithTimeout(ctx, time.Duration(effTaskTimeout)*time.Second)
	defer cancelExec()

	// 登录阶段采用设备连接超时窗口；若未设置则回退到任务窗口
	devTO := req.DeviceTimeoutSec
	if devTO <= 0 {
		devTO = effTaskTimeout
	}

	var loginCtx context.Context = execCtx
	var cancelLogin context.CancelFunc
	// 若设备连接超时短于任务超时，则创建更短的登录上下文
	if time.Duration(devTO)*time.Second < time.Duration(effTaskTimeout)*time.Second {
		loginCtx, cancelLogin = context.WithTimeout(ctx, time.Duration(devTO)*time.Second)
		defer cancelLogin()
	} else {
		// 若父上下文更紧，则以父上下文为准
		if deadline, ok := ctx.Deadline(); ok {
			remain := time.Until(deadline)
			if remain > 0 && remain < time.Duration(effTaskTimeout)*time.Second {
				loginCtx = ctx
			}
		}
	}

	client, err := b.pool.GetConnection(loginCtx, conn)
	if err != nil {
		// 设备登陆阶段的超时错误，统一标注为“设备登陆失败”
		if isLoginTimeout(err) {
			return nil, fmt.Errorf("设备登陆失败")
		}
		return nil, fmt.Errorf("failed to create SSH connection: %w", err)
	}
	defer b.pool.ReleaseConnection(conn)

	// 注入平台级预命令（enable 与分页关闭）
	commands := make([]string, 0, len(userCommands)+4)
	pre := b.getPreCommands(req.DevicePlatform, userCommands)
	if len(pre) > 0 {
		commands = append(commands, pre...)
	}
	if len(userCommands) > 0 {
		commands = append(commands, userCommands...)
	}

	// 交互默认与提示符后缀
	defaults := getPlatformDefaults(strings.ToLower(strings.TrimSpace(func() string {
		if req.DevicePlatform == "" {
			return "default"
		}
		return req.DevicePlatform
	}())))
	promptSuffixes := defaults.PromptSuffixes
	if len(promptSuffixes) == 0 {
		promptSuffixes = []string{"#", ">", "]"}
	}

	// 构造交互选项，包括 enable 流程与自动交互
	interactive := &ssh.InteractiveOptions{SkipDelayedEcho: defaults.SkipDelayedEcho}
	// 新增：用于精确提示符判定
	interactive.DeviceName = strings.TrimSpace(req.DeviceName)
	interactive.PromptSuffixes = promptSuffixes
	// enable 配置
	p := strings.ToLower(strings.TrimSpace(req.DevicePlatform))
	if dd, ok := b.cfg.Collector.DeviceDefaults[p]; ok && dd.EnableRequired {
		interactive.EnableCLI = strings.TrimSpace(dd.EnableCLI)
		interactive.EnableExpectOutput = strings.TrimSpace(dd.EnableExceptOutput)
		if strings.TrimSpace(req.EnablePassword) != "" {
			interactive.EnablePassword = strings.TrimSpace(req.EnablePassword)
		} else if strings.TrimSpace(req.Password) != "" {
			interactive.EnablePassword = strings.TrimSpace(req.Password)
		}
	} else if strings.HasPrefix(p, "cisco") {
		// 兼容 Cisco 默认行为
		if strings.TrimSpace(req.EnablePassword) != "" {
			interactive.EnablePassword = strings.TrimSpace(req.EnablePassword)
		} else if strings.TrimSpace(req.Password) != "" {
			interactive.EnablePassword = strings.TrimSpace(req.Password)
		}
		if strings.TrimSpace(interactive.EnableCLI) == "" {
			interactive.EnableCLI = "enable"
		}
		if strings.TrimSpace(interactive.EnableExpectOutput) == "" {
			interactive.EnableExpectOutput = "Password"
		}
	}
	if strings.TrimSpace(req.Password) != "" {
		interactive.LoginPassword = strings.TrimSpace(req.Password)
	}
	if defaults.CommandIntervalMS > 0 {
		interactive.CommandIntervalMS = defaults.CommandIntervalMS
	}
	// 新增：交互时序与节奏参数映射
	if defaults.CommandTimeoutSec > 0 {
		interactive.PerCommandTimeoutSec = defaults.CommandTimeoutSec
	}
	if defaults.QuietAfterMS > 0 {
		interactive.QuietAfterMS = defaults.QuietAfterMS
	}
	if defaults.QuietPollIntervalMS > 0 {
		interactive.QuietPollIntervalMS = defaults.QuietPollIntervalMS
	}
	if defaults.EnablePasswordFallbackMS > 0 {
		interactive.EnablePasswordFallbackMS = defaults.EnablePasswordFallbackMS
	}
	if defaults.PromptInducerIntervalMS > 0 {
		interactive.PromptInducerIntervalMS = defaults.PromptInducerIntervalMS
	}
	if defaults.PromptInducerMaxCount > 0 {
		interactive.PromptInducerMaxCount = defaults.PromptInducerMaxCount
	}
	if defaults.ExitPauseMS > 0 {
		interactive.ExitPauseMS = defaults.ExitPauseMS
	}
	if len(defaults.AutoInteractions) > 0 {
		mapped := make([]ssh.AutoInteraction, 0, len(defaults.AutoInteractions))
		for _, ai := range defaults.AutoInteractions {
			if strings.TrimSpace(ai.ExpectOutput) == "" || strings.TrimSpace(ai.AutoSend) == "" {
				continue
			}
			mapped = append(mapped, ssh.AutoInteraction{ExpectOutput: ai.ExpectOutput, AutoSend: ai.AutoSend})
		}
		interactive.AutoInteractions = mapped
	}
	// 不再叠加全局交互；交互配置由平台/device_defaults.interact 提供

	// 交互优先执行
	res, err := client.ExecuteInteractiveCommands(execCtx, commands, promptSuffixes, interactive)
	if err != nil {
		// 回退前重置连接，避免复用异常会话
		_ = b.pool.CloseConnection(conn)
		// 重连使用与登录相同的限时窗口
		client2, errConn := b.pool.GetConnection(loginCtx, conn)
		if errConn != nil {
			// 若重连失败，保留原始错误以便定位
			return nil, fmt.Errorf("interactive failed: %v; fallback reconnect failed: %w", err, errConn)
		}
		defer b.pool.ReleaseConnection(conn)
		// 回退非交互（保证尽力而为）
		res2, err2 := client2.ExecuteCommands(execCtx, commands)
		if err2 != nil {
			return nil, fmt.Errorf("interactive failed: %v; non-interactive failed: %w", err, err2)
		}
		// 回退结果继续走统一过滤流程
		filtered := filterInternalPreCommandsBase(b.cfg, req.DevicePlatform, userCommands, res2)
		out := make([]*ssh.CommandResult, 0, len(filtered))
		for _, r := range filtered {
			if r == nil {
				continue
			}
			nr := *r
			nr.Output = applyPlatformLineFilter(b.cfg, req.DevicePlatform, r.Output)
			out = append(out, &nr)
		}
		return out, nil
	}

	// 正常交互结果：统一过滤与输出处理
	filtered := filterInternalPreCommandsBase(b.cfg, req.DevicePlatform, userCommands, res)
	out := make([]*ssh.CommandResult, 0, len(filtered))
	for _, r := range filtered {
		if r == nil {
			continue
		}
		nr := *r
		nr.Output = applyPlatformLineFilter(b.cfg, req.DevicePlatform, r.Output)
		out = append(out, &nr)
	}
	return out, nil
}

// isLoginTimeout 判断连接/握手阶段是否为典型超时错误
func isLoginTimeout(err error) bool {
	if err == nil {
		return false
	}
	// 上下文超时
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// 网络层超时（包括 i/o timeout 等）
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	// 字符串兜底匹配
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "i/o timeout") {
		return true
	}
	return false
}

// filterInternalPreCommandsBase 基于配置过滤预命令结果（enable 与关闭分页），保留用户命令对应输出
func filterInternalPreCommandsBase(cfg *config.Config, platform string, userCmds []string, results []*ssh.CommandResult) []*ssh.CommandResult {
	out := make([]*ssh.CommandResult, 0, len(results))
	p := strings.ToLower(strings.TrimSpace(platform))
	dd, ok := cfg.Collector.DeviceDefaults[p]
	if !ok {
		if strings.HasPrefix(p, "huawei") {
			dd, ok = cfg.Collector.DeviceDefaults["huawei"]
		}
		if !ok && strings.HasPrefix(p, "h3c") {
			dd, ok = cfg.Collector.DeviceDefaults["h3c"]
		}
		if !ok && strings.HasPrefix(p, "cisco") {
			dd, ok = cfg.Collector.DeviceDefaults["cisco_ios"]
		}
		if !ok && strings.HasPrefix(p, "linux") {
			dd = cfg.Collector.DeviceDefaults["linux"]
		}
	}
	// 用户命令集合用于硬过滤未知命令
	uidx := map[string]struct{}{}
	for _, u := range userCmds {
		uidx[strings.ToLower(strings.TrimSpace(u))] = struct{}{}
	}
	enableCmd := strings.TrimSpace(dd.EnableCLI)
	if enableCmd == "" {
		enableCmd = "enable"
	}
	for _, r := range results {
		if r == nil {
			continue
		}
		cmd := strings.ToLower(strings.TrimSpace(r.Command))
		// 跳过 enable 与关闭分页预命令
		if cmd == strings.ToLower(enableCmd) {
			continue
		}
		skip := false
		for _, pc := range dd.DisablePagingCmds {
			if strings.ToLower(strings.TrimSpace(pc)) == cmd {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// 若命令不在用户列表，尽量跳过（避免误入内部命令）
		if _, ok := uidx[cmd]; !ok {
			continue
		}
		out = append(out, r)
	}
	// 若过滤后为空，则回退原结果（尽量保留）
	if len(out) == 0 {
		return results
	}
	return out
}

// getPreCommands 生成平台预命令（避免与用户重复）
func (b *InteractBasic) getPreCommands(platform string, user []string) []string {
	out := make([]string, 0, 4)
	p := strings.ToLower(strings.TrimSpace(platform))
	if p == "" {
		return out
	}
	dd, ok := b.cfg.Collector.DeviceDefaults[p]
	if !ok {
		if strings.HasPrefix(p, "huawei") {
			dd, ok = b.cfg.Collector.DeviceDefaults["huawei"]
		}
		if !ok && strings.HasPrefix(p, "h3c") {
			dd, ok = b.cfg.Collector.DeviceDefaults["h3c"]
		}
		if !ok && strings.HasPrefix(p, "cisco") {
			dd, ok = b.cfg.Collector.DeviceDefaults["cisco_ios"]
		}
		if !ok && strings.HasPrefix(p, "linux") {
			dd, ok = b.cfg.Collector.DeviceDefaults["linux"]
		}
	}
	has := func(cmd string) bool {
		key := strings.ToLower(strings.TrimSpace(cmd))
		for _, c := range user {
			if strings.ToLower(strings.TrimSpace(c)) == key {
				return true
			}
		}
		return false
	}
	if ok && dd.EnableRequired {
		ecmd := strings.TrimSpace(dd.EnableCLI)
		if ecmd == "" {
			ecmd = "enable"
		}
		if !has(ecmd) {
			out = append(out, ecmd)
		}
	}
	for _, pc := range dd.DisablePagingCmds {
		if strings.TrimSpace(pc) == "" {
			continue
		}
		if !has(pc) {
			out = append(out, pc)
		}
	}
	return out
}

// EnterConfigMode 统一进入配置模式：读取平台 config_mode_clis 并执行
func (b *InteractBasic) EnterConfigMode(ctx context.Context, req *ExecRequest) ([]*ssh.CommandResult, error) {
    if b == nil || b.cfg == nil || b.pool == nil { return nil, fmt.Errorf("InteractBasic not initialized") }
    p := strings.ToLower(strings.TrimSpace(func() string { if req.DevicePlatform == "" { return "default" }; return req.DevicePlatform }()))
    dd, ok := b.cfg.Collector.DeviceDefaults[p]
    if !ok {
        found := false
        if strings.HasPrefix(p, "huawei") {
            if v, ok2 := b.cfg.Collector.DeviceDefaults["huawei"]; ok2 { dd = v; found = true }
        }
        if !found && strings.HasPrefix(p, "h3c") {
            if v, ok2 := b.cfg.Collector.DeviceDefaults["h3c"]; ok2 { dd = v; found = true }
        }
        if !found && strings.HasPrefix(p, "cisco") {
            if v, ok2 := b.cfg.Collector.DeviceDefaults["cisco_ios"]; ok2 { dd = v; found = true }
        }
        if !found && strings.HasPrefix(p, "linux") {
            if v, ok2 := b.cfg.Collector.DeviceDefaults["linux"]; ok2 { dd = v; found = true }
        }
    }
    cmds := make([]string, 0, len(dd.ConfigModeCLIs))
    for _, c := range dd.ConfigModeCLIs { t := strings.TrimSpace(c); if t != "" { cmds = append(cmds, t) } }
    if len(cmds) == 0 { return nil, nil }

    // 连接复用与上下文
    effTaskTimeout := req.TaskTimeoutSec; if effTaskTimeout <= 0 { effTaskTimeout = 30 }
    execCtx, cancelExec := context.WithTimeout(ctx, time.Duration(effTaskTimeout)*time.Second); defer cancelExec()
    devTO := req.DeviceTimeoutSec; if devTO <= 0 { devTO = effTaskTimeout }
    var loginCtx context.Context = execCtx; var cancelLogin context.CancelFunc
    if time.Duration(devTO)*time.Second < time.Duration(effTaskTimeout)*time.Second {
        loginCtx, cancelLogin = context.WithTimeout(ctx, time.Duration(devTO)*time.Second); defer cancelLogin()
    } else {
        if deadline, ok := ctx.Deadline(); ok { remain := time.Until(deadline); if remain > 0 && remain < time.Duration(effTaskTimeout)*time.Second { loginCtx = ctx } }
    }
    conn := &ssh.ConnectionInfo{ Host: req.DeviceIP, Port: func() int { if req.Port < 1 || req.Port > 65535 { return 22 }; return req.Port }(), Username: req.UserName, Password: req.Password }
    client, err := b.pool.GetConnection(loginCtx, conn)
    if err != nil { if isLoginTimeout(err) { return nil, fmt.Errorf("设备登陆失败") }; return nil, fmt.Errorf("failed to create SSH connection: %w", err) }
    defer b.pool.ReleaseConnection(conn)

    // 平台交互参数（与 Execute 一致）
    defaults := getPlatformDefaults(p)
    promptSuffixes := defaults.PromptSuffixes; if len(promptSuffixes) == 0 { promptSuffixes = []string{"#", ">", "]"} }
    interactive := &ssh.InteractiveOptions{ SkipDelayedEcho: defaults.SkipDelayedEcho }
    // 新增：用于精确提示符判定
    interactive.DeviceName = strings.TrimSpace(req.DeviceName)
    interactive.PromptSuffixes = promptSuffixes
    if dd.EnableRequired {
        interactive.EnableCLI = strings.TrimSpace(dd.EnableCLI)
        interactive.EnableExpectOutput = strings.TrimSpace(dd.EnableExceptOutput)
        if strings.TrimSpace(req.EnablePassword) != "" { interactive.EnablePassword = strings.TrimSpace(req.EnablePassword) } else if strings.TrimSpace(req.Password) != "" { interactive.EnablePassword = strings.TrimSpace(req.Password) }
    }
    if strings.TrimSpace(req.Password) != "" { interactive.LoginPassword = strings.TrimSpace(req.Password) }
    if defaults.CommandIntervalMS > 0 { interactive.CommandIntervalMS = defaults.CommandIntervalMS }
    if defaults.CommandTimeoutSec > 0 { interactive.PerCommandTimeoutSec = defaults.CommandTimeoutSec }
    if defaults.QuietAfterMS > 0 { interactive.QuietAfterMS = defaults.QuietAfterMS }
    if defaults.QuietPollIntervalMS > 0 { interactive.QuietPollIntervalMS = defaults.QuietPollIntervalMS }
    if defaults.EnablePasswordFallbackMS > 0 { interactive.EnablePasswordFallbackMS = defaults.EnablePasswordFallbackMS }
    if defaults.PromptInducerIntervalMS > 0 { interactive.PromptInducerIntervalMS = defaults.PromptInducerIntervalMS }
    if defaults.PromptInducerMaxCount > 0 { interactive.PromptInducerMaxCount = defaults.PromptInducerMaxCount }
    if defaults.ExitPauseMS > 0 { interactive.ExitPauseMS = defaults.ExitPauseMS }
    // 退出命令序列（会话结束时使用）
    if strings.HasPrefix(p, "cisco") { interactive.ExitCommands = []string{"exit"} } else if strings.HasPrefix(p, "h3c") || strings.HasPrefix(p, "huawei") { interactive.ExitCommands = []string{"quit", "exit"} } else { interactive.ExitCommands = []string{"exit", "quit"} }

    // 交互执行进入配置模式命令，失败则回退到非交互执行
    res, err := client.ExecuteInteractiveCommands(execCtx, cmds, promptSuffixes, interactive)
    if err != nil {
        _ = b.pool.CloseConnection(conn)
        client2, errConn := b.pool.GetConnection(loginCtx, conn)
        if errConn != nil { return nil, fmt.Errorf("interactive failed: %v; fallback reconnect failed: %w", err, errConn) }
        defer b.pool.ReleaseConnection(conn)
        res2, err2 := client2.ExecuteCommands(execCtx, cmds)
        if err2 != nil { return nil, fmt.Errorf("interactive failed: %v; non-interactive failed: %w", err, err2) }
        return res2, nil
    }
    return res, nil
}

// isUserOrPrivPrompt 判断是否“仅主机名 + 提示符后缀”，用于退出配置模式成功判断
func isUserOrPrivPrompt(line string, suffixes []string) bool {
    s := strings.TrimSpace(line)
    if s == "" { return false }
    found := false
    for _, suf := range suffixes { if strings.HasSuffix(s, suf) { found = true; break } }
    if !found { return false }
    l := strings.ToLower(s)
    // 排除典型配置模式特征：包含 "["（如 [HUAWEI]）或 "("（如 (config)）
    if strings.Contains(l, "[") || strings.Contains(l, "(") || strings.Contains(l, "config") { return false }
    return true
}
