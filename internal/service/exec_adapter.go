package service

import (
    "context"
    "fmt"
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
    TimeoutSec      int
}

// ExecAdapter 适配 ssh.Client 交互/非交互批处理
type ExecAdapter struct {
    pool *ssh.Pool
    cfg  *config.Config
}

func NewExecAdapter(pool *ssh.Pool, cfg *config.Config) *ExecAdapter {
    return &ExecAdapter{pool: pool, cfg: cfg}
}

// Execute 执行命令（优先交互，失败回退非交互）
func (a *ExecAdapter) Execute(ctx context.Context, req *ExecRequest, userCommands []string) ([]*ssh.CommandResult, error) {
    if strings.TrimSpace(req.CollectProtocol) == "" {
        req.CollectProtocol = "ssh"
    }
    if strings.ToLower(req.CollectProtocol) != "ssh" {
        return nil, fmt.Errorf("unsupported protocol: %s", req.CollectProtocol)
    }

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

    // 获取一次连接（单次登录执行整批命令）
    client, err := a.pool.GetConnection(ctx, conn)
    if err != nil {
        return nil, fmt.Errorf("failed to create SSH connection: %w", err)
    }
    defer a.pool.ReleaseConnection(conn)

    // 注入平台级预命令（enable 与分页关闭）
    commands := make([]string, 0, len(userCommands)+4)
    pre := a.getPreCommands(req.DevicePlatform, userCommands)
    if len(pre) > 0 {
        commands = append(commands, pre...)
    }
    if len(userCommands) > 0 {
        commands = append(commands, userCommands...)
    }

    // 交互默认与提示符后缀
    defaults := getPlatformDefaults(strings.ToLower(strings.TrimSpace(func() string {
        if req.DevicePlatform == "" { return "default" }
        return req.DevicePlatform
    }())))
    promptSuffixes := defaults.PromptSuffixes
    if len(promptSuffixes) == 0 {
        promptSuffixes = []string{"#", ">", "]"}
    }

    // 构造交互选项，包括 enable 流程与自动交互
    interactive := &ssh.InteractiveOptions{SkipDelayedEcho: defaults.SkipDelayedEcho}
    // 退出命令按平台设置
    p := strings.ToLower(strings.TrimSpace(req.DevicePlatform))
    if strings.HasPrefix(p, "cisco") {
        interactive.ExitCommands = []string{"exit"}
    } else if strings.HasPrefix(p, "h3c") || strings.HasPrefix(p, "huawei") {
        interactive.ExitCommands = []string{"quit", "exit"}
    } else {
        interactive.ExitCommands = []string{"exit", "quit"}
    }
    // enable 配置
    if dd, ok := a.cfg.Collector.DeviceDefaults[p]; ok && dd.EnableRequired {
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
        if strings.TrimSpace(interactive.EnableCLI) == "" { interactive.EnableCLI = "enable" }
        if strings.TrimSpace(interactive.EnableExpectOutput) == "" { interactive.EnableExpectOutput = "Password" }
    }
    if defaults.CommandIntervalMS > 0 { interactive.CommandIntervalMS = defaults.CommandIntervalMS }
    if len(defaults.AutoInteractions) > 0 {
        mapped := make([]ssh.AutoInteraction, 0, len(defaults.AutoInteractions))
        for _, ai := range defaults.AutoInteractions {
            if strings.TrimSpace(ai.ExpectOutput) == "" || strings.TrimSpace(ai.AutoSend) == "" { continue }
            mapped = append(mapped, ssh.AutoInteraction{ExpectOutput: ai.ExpectOutput, AutoSend: ai.AutoSend})
        }
        interactive.AutoInteractions = mapped
    }
    // 叠加配置层的自动交互
    if cfgAIs := a.cfg.Collector.Interact.AutoInteractions; len(cfgAIs) > 0 {
        idx := map[string]int{}
        for i, ai := range interactive.AutoInteractions {
            key := strings.ToLower(strings.TrimSpace(ai.ExpectOutput)); if key != "" { idx[key] = i }
        }
        for _, ai := range cfgAIs {
            eo := strings.TrimSpace(ai.ExpectOutput)
            as := strings.TrimSpace(ai.AutoSend)
            if eo == "" || as == "" { continue }
            key := strings.ToLower(eo)
            if pos, ok := idx[key]; ok {
                interactive.AutoInteractions[pos] = ssh.AutoInteraction{ExpectOutput: eo, AutoSend: as}
            } else {
                interactive.AutoInteractions = append(interactive.AutoInteractions, ssh.AutoInteraction{ExpectOutput: eo, AutoSend: as})
            }
        }
    }

    // 任务超时控制
    effTimeout := req.TimeoutSec
    if effTimeout <= 0 { effTimeout = 30 }
    execCtx, cancel := context.WithTimeout(ctx, time.Duration(effTimeout)*time.Second)
    defer cancel()

    // 交互优先执行
    res, err := client.ExecuteInteractiveCommands(execCtx, commands, promptSuffixes, interactive)
    if err == nil {
        return res, nil
    }
    // 回退非交互（保证尽力而为）
    res2, err2 := client.ExecuteCommands(execCtx, commands)
    if err2 != nil {
        return nil, fmt.Errorf("interactive failed: %v; non-interactive failed: %w", err, err2)
    }
    return res2, nil
}

// getPreCommands 生成平台预命令（避免与用户重复）
func (a *ExecAdapter) getPreCommands(platform string, user []string) []string {
    out := make([]string, 0, 4)
    p := strings.ToLower(strings.TrimSpace(platform))
    if p == "" { return out }
    dd, ok := a.cfg.Collector.DeviceDefaults[p]
    if !ok {
        if strings.HasPrefix(p, "huawei") { dd, ok = a.cfg.Collector.DeviceDefaults["huawei"] }
        if !ok && strings.HasPrefix(p, "h3c") { dd, ok = a.cfg.Collector.DeviceDefaults["h3c"] }
        if !ok && strings.HasPrefix(p, "cisco") { dd, ok = a.cfg.Collector.DeviceDefaults["cisco_ios"] }
        if !ok && strings.HasPrefix(p, "linux") { dd, ok = a.cfg.Collector.DeviceDefaults["linux"] }
    }
    has := func(cmd string) bool {
        key := strings.ToLower(strings.TrimSpace(cmd))
        for _, c := range user { if strings.ToLower(strings.TrimSpace(c)) == key { return true } }
        return false
    }
    if ok && dd.EnableRequired {
        ecmd := strings.TrimSpace(dd.EnableCLI)
        if ecmd == "" { ecmd = "enable" }
        if !has(ecmd) { out = append(out, ecmd) }
    }
    for _, pc := range dd.DisablePagingCmds {
        if strings.TrimSpace(pc) == "" { continue }
        if !has(pc) { out = append(out, pc) }
    }
    return out
}