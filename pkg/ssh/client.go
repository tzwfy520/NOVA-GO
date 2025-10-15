package ssh

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Config SSH配置
type Config struct {
	Timeout     time.Duration `yaml:"timeout"`
	KeepAlive   time.Duration `yaml:"keep_alive"`
	MaxSessions int           `yaml:"max_sessions"`
}

// Client SSH客户端
type Client struct {
    config     *Config
    connection *ssh.Client
    sessions   map[string]*ssh.Session
    mutex      sync.RWMutex
    // 保存最近一次成功连接的参数，用于在会话创建失败（如 EOF）时自动重连
    info       *ConnectionInfo
}

// ConnectionInfo SSH连接信息
type ConnectionInfo struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	KeyFile  string `json:"key_file,omitempty"`
}

// CommandResult 命令执行结果
type CommandResult struct {
	Command  string        `json:"command"`
	Output   string        `json:"output"`
	Error    string        `json:"error"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
}

// InteractiveOptions 交互会话选项
// 目前支持在执行 "enable" 时识别密码提示并自动输入 enable 密码
type InteractiveOptions struct {
	EnablePassword string
	ExitCommands   []string
	// 新增：命令间隔与自动交互
	CommandIntervalMS int
	AutoInteractions  []AutoInteraction
}

// AutoInteraction 自动交互对
// 当输出包含 ExpectOutput（大小写不敏感）时，自动发送 AutoSend（通常为空格或确认）
type AutoInteraction struct {
	ExpectOutput string
	AutoSend     string
}

// NewClient 创建SSH客户端
func NewClient(config *Config) *Client {
	return &Client{
		config:   config,
		sessions: make(map[string]*ssh.Session),
	}
}

// Connect 连接SSH服务器
func (c *Client) Connect(ctx context.Context, info *ConnectionInfo) error {
    c.mutex.Lock()
    defer c.mutex.Unlock()

    // 记录连接参数以便后续自动重连
    c.info = info

	// 构建SSH配置
	sshConfig := &ssh.ClientConfig{
		User:            info.Username,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         c.config.Timeout,
		Config: ssh.Config{
			// 支持旧版本的密钥交换算法
			KeyExchanges: []string{
				"diffie-hellman-group14-sha256",
				"diffie-hellman-group14-sha1",
				"diffie-hellman-group1-sha1",
				"diffie-hellman-group-exchange-sha256",
				"diffie-hellman-group-exchange-sha1",
				"ecdh-sha2-nistp256",
				"ecdh-sha2-nistp384",
				"ecdh-sha2-nistp521",
			},
			// 支持旧版本的加密算法
			Ciphers: []string{
				"aes128-ctr",
				"aes192-ctr",
				"aes256-ctr",
				"aes128-gcm@openssh.com",
				"aes256-gcm@openssh.com",
				"aes128-cbc",
				"aes192-cbc",
				"aes256-cbc",
				"3des-cbc",
			},
			// 支持旧版本的MAC算法
			MACs: []string{
				"hmac-sha2-256-etm@openssh.com",
				"hmac-sha2-256",
				"hmac-sha1",
				"hmac-sha1-96",
			},
		},
		// 支持旧版本主机密钥算法
		HostKeyAlgorithms: []string{
			"ssh-rsa",
			"rsa-sha2-256",
			"rsa-sha2-512",
			"ecdsa-sha2-nistp256",
			"ecdsa-sha2-nistp384",
			"ecdsa-sha2-nistp521",
		},
	}

	// 设置认证方式
	if info.Password != "" {
		// 同时尝试 password 与 keyboard-interactive，提高与网络设备的兼容性
		sshConfig.Auth = []ssh.AuthMethod{
			ssh.Password(info.Password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				// 对所有提示统一使用密码响应（常见于 H3C/Cisco 等设备）
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = info.Password
				}
				return answers, nil
			}),
		}
	}

	if info.KeyFile != "" {
		// TODO: 实现密钥文件认证
	}

	// 连接SSH服务器
	address := fmt.Sprintf("%s:%d", info.Host, info.Port)

	// 使用context控制连接超时
	dialer := &net.Dialer{
		Timeout: c.config.Timeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, sshConfig)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create SSH connection: %w", err)
	}

	c.connection = ssh.NewClient(sshConn, chans, reqs)

    // 启动保活机制
    go c.keepAlive(ctx)

    return nil
}

// newSessionWithRetry 创建会话（带重试）
// 针对部分网络设备首次或快速连续打开会话通道可能返回
// "ssh: rejected: administratively prohibited (open failed)" 的情况，
// 进行短延迟重试以提高稳定性。
func (c *Client) newSessionWithRetry() (*ssh.Session, error) {
    if c.connection == nil {
        return nil, fmt.Errorf("SSH connection not established")
    }

    // 退避策略：立即、200ms、500ms、1s、2s，共5次
    backoffs := []time.Duration{0, 200 * time.Millisecond, 500 * time.Millisecond, 1 * time.Second, 2 * time.Second}
    var lastErr error
    for _, d := range backoffs {
        if d > 0 {
            time.Sleep(d)
        }
        sess, err := c.connection.NewSession()
        if err == nil {
            return sess, nil
        }
        lastErr = err
        // 若错误不是通道被管理拒绝/打开失败，继续有限次重试以防瞬时状态
        // 主要针对 "administratively prohibited"/"open failed" 文案做退避
        msg := strings.ToLower(err.Error())
        // 包含 EOF 也作为可重试错误（部分设备在登录后短时间内打开会话会返回 EOF）
        if strings.Contains(msg, "eof") && c.info != nil {
            // 尝试一次自动重连：关闭旧连接后根据保存的参数重建连接
            // 使用 SSH 配置的 Timeout 作为重连的超时窗口
            _ = c.Close()
            ctx, cancel := context.WithTimeout(context.Background(), c.config.Timeout)
            // 忽略重连错误并继续后续退避，如果重连成功则下一次循环可能成功创建会话
            _ = c.Connect(ctx, c.info)
            cancel()
            // 短暂等待以让设备端就绪
            time.Sleep(200 * time.Millisecond)
            // 继续进入下一次退避尝试
            continue
        }
        // 非典型错误也尝试后续退避，但不额外处理
    }
    return nil, lastErr
}

// ExecuteCommand 执行单个命令
func (c *Client) ExecuteCommand(ctx context.Context, command string) (*CommandResult, error) {
    if c.connection == nil {
        return nil, fmt.Errorf("SSH connection not established")
    }

	startTime := time.Now()
	result := &CommandResult{
		Command: command,
	}

    // 创建会话（带重试）
    session, err := c.newSessionWithRetry()
    if err != nil {
        result.Error = fmt.Sprintf("failed to create session: %v", err)
        result.ExitCode = -1
        return result, err
    }
	defer session.Close()

	// 执行命令
	output, err := session.CombinedOutput(command)
	result.Duration = time.Since(startTime)
	result.Output = string(output)

	if err != nil {
		result.Error = err.Error()
		if exitError, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitError.ExitStatus()
		} else {
			result.ExitCode = -1
		}
		return result, err
	}

	result.ExitCode = 0
	return result, nil
}

// ExecuteCommands 批量执行命令
func (c *Client) ExecuteCommands(ctx context.Context, commands []string) ([]*CommandResult, error) {
	results := make([]*CommandResult, 0, len(commands))

	for _, command := range commands {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result, err := c.ExecuteCommand(ctx, command)
		results = append(results, result)

		// 如果命令执行失败，记录错误但继续执行后续命令
		if err != nil {
			// 可以根据需要决定是否继续执行
			continue
		}
	}

	return results, nil
}

// ExecuteInteractiveCommand 执行交互式命令
func (c *Client) ExecuteInteractiveCommand(ctx context.Context, command string, responses []string) (*CommandResult, error) {
    if c.connection == nil {
        return nil, fmt.Errorf("SSH connection not established")
    }

	startTime := time.Now()
	result := &CommandResult{
		Command: command,
	}

    // 创建会话（带重试）
    session, err := c.newSessionWithRetry()
    if err != nil {
        result.Error = fmt.Sprintf("failed to create session: %v", err)
        result.ExitCode = -1
        return result, err
    }
	defer session.Close()

	// 设置终端模式（启用回显，兼容网络设备CLI），并使用终端类型回退
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // 启用回显
		ssh.TTY_OP_ISPEED: 14400, // 输入速度
		ssh.TTY_OP_OSPEED: 14400, // 输出速度
	}

	{
		var lastErr error
		for _, term := range []string{"vt100", "xterm", "ansi", "dumb"} {
			if ptyErr := session.RequestPty(term, 80, 24, modes); ptyErr == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			result.Error = fmt.Sprintf("failed to request pty: %v", lastErr)
			result.ExitCode = -1
			return result, lastErr
		}
	}

	// 获取输入输出管道
	stdin, err := session.StdinPipe()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get stdin: %v", err)
		result.ExitCode = -1
		return result, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get stdout: %v", err)
		result.ExitCode = -1
		return result, err
	}

	// 启动命令
	if err := session.Start(command); err != nil {
		result.Error = fmt.Sprintf("failed to start command: %v", err)
		result.ExitCode = -1
		return result, err
	}

	// 处理交互
	var output strings.Builder
	done := make(chan error, 1)

	go func() {
		defer stdin.Close()
		for _, response := range responses {
			time.Sleep(100 * time.Millisecond) // 等待命令准备
			// 网络设备通常期望 CRLF
			_, _ = stdin.Write([]byte(response + "\r\n"))
		}
	}()

	go func() {
		buffer := make([]byte, 1024)
		for {
			n, err := stdout.Read(buffer)
			if n > 0 {
				output.Write(buffer[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		done <- session.Wait()
	}()

	// 等待命令完成或超时
	select {
	case err := <-done:
		result.Duration = time.Since(startTime)
		result.Output = output.String()
		if err != nil {
			result.Error = err.Error()
			if exitError, ok := err.(*ssh.ExitError); ok {
				result.ExitCode = exitError.ExitStatus()
			} else {
				result.ExitCode = -1
			}
			return result, err
		}
		result.ExitCode = 0
		return result, nil
	case <-ctx.Done():
		session.Signal(ssh.SIGTERM)
		result.Duration = time.Since(startTime)
		result.Output = output.String()
		result.Error = "command timeout"
		result.ExitCode = -1
		return result, ctx.Err()
	}
}

// ExecuteInteractiveCommands 在单一交互式会话(PTY Shell)中串行执行多条命令
// 使用启发式的提示符后缀来分隔每条命令的输出 (例如: '>', '#', ']')
func (c *Client) ExecuteInteractiveCommands(ctx context.Context, commands []string, promptSuffixes []string, opts *InteractiveOptions) ([]*CommandResult, error) {
    if c.connection == nil {
        return nil, fmt.Errorf("SSH connection not established")
    }

    // 创建会话（带重试）
    session, err := c.newSessionWithRetry()
    if err != nil {
        return nil, fmt.Errorf("failed to create session: %w", err)
    }
	// 确保会话在函数结束时被关闭
	defer session.Close()

	// 设置终端模式（启用回显，兼容网络设备CLI）
	// 同时增加终端类型回退，优先使用 vt100 再尝试 xterm/ansi/dumb
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	{
		var lastErr error
		for _, term := range []string{"vt100", "xterm", "ansi", "dumb"} {
			if ptyErr := session.RequestPty(term, 80, 24, modes); ptyErr == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			session.Close()
			return nil, fmt.Errorf("failed to request pty: %w", lastErr)
		}
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdin: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stdout: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to get stderr: %w", err)
	}

	// 启动交互式Shell
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	// 发送 CRLF 促使设备输出当前提示符，便于后续检测（网络设备通常期望 CRLF）
	_, _ = stdin.Write([]byte("\r\n"))

	// 提示符诱发器：在初始阶段定期发送 CRLF，帮助设备输出提示符
	// 某些设备在建立 PTY 后需要键入回车才能显示提示符
	stopTrigger := make(chan struct{})
	go func() {
		defer func() { recover() }()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		// 最多诱发 12 次（~12s），避免过度刷屏
		count := 0
		for {
			select {
			case <-stopTrigger:
				return
			case <-ticker.C:
				if count >= 12 {
					return
				}
				_, _ = stdin.Write([]byte("\r\n"))
				count++
			}
		}
	}()

	// 读取输出的协程，将数据按行推送到通道
	lineCh := make(chan string, 4096)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		buf := make([]byte, 2048)
		var acc strings.Builder
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				acc.Write(buf[:n])
				s := acc.String()
				// 统一换行符：仅将 CRLF -> \n；保留孤立 CR 作为行续行（去除），避免将回车误判为换行
				s = strings.ReplaceAll(s, "\r\n", "\n")
				s = strings.ReplaceAll(s, "\r", "")
				// 按换行切分
				lines := strings.Split(s, "\n")
				// 保留最后一部分(可能不完整)
				acc.Reset()
				if len(lines) > 0 {
					acc.WriteString(lines[len(lines)-1])
				}
				for i := 0; i < len(lines)-1; i++ {
					line := strings.TrimSpace(lines[i])
					// 阻塞推送，避免丢失关键信息（例如提示符）
					lineCh <- line
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// 同步读取 stderr，合并到同一行通道进行提示符检测
	go func() {
		buf := make([]byte, 2048)
		var acc strings.Builder
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				acc.Write(buf[:n])
				s := acc.String()
				// 统一换行符：仅将 CRLF -> \n；孤立 CR 去除，避免命令回显被拆成多行
				s = strings.ReplaceAll(s, "\r\n", "\n")
				s = strings.ReplaceAll(s, "\r", "")
				lines := strings.Split(s, "\n")
				acc.Reset()
				if len(lines) > 0 {
					acc.WriteString(lines[len(lines)-1])
				}
				for i := 0; i < len(lines)-1; i++ {
					line := strings.TrimSpace(lines[i])
					lineCh <- line
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// 辅助函数：清洗行内容，移除 ANSI 转义序列与不可见控制符，便于稳定提示符检测
	sanitize := func(s string) string {
		// 移除常见 ANSI 转义序列，如 \x1b[31m、\x1b[0K 等
		// 简单处理：逐段过滤 ESC 开头的控制序列
		b := make([]rune, 0, len(s))
		skip := false
		for i := 0; i < len(s); i++ {
			ch := s[i]
			if skip {
				// 跳过直到命令字符结尾（以字母结尾的 CSI 序列）
				if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
					skip = false
				}
				continue
			}
			if ch == 0x1b { // ESC
				skip = true
				continue
			}
			// 过滤其他不可见控制字符（<0x20，除换行与回车已被统一处理）
			if ch < 0x20 && ch != '\t' { // 保留制表符以防列对齐
				continue
			}
			b = append(b, rune(ch))
		}
		return strings.TrimSpace(string(b))
	}

	// 捕获首个提示符的主机名前缀，用于后续更稳健的提示符判断
	var promptPrefix string

	// 辅助函数：判断行是否是提示符（先清洗再匹配后缀；若已捕获前缀，则要求包含前缀）
	isPrompt := func(line string) bool {
		trimmed := sanitize(line)
		if trimmed == "" {
			return false
		}
		for _, suf := range promptSuffixes {
			if strings.HasSuffix(trimmed, suf) {
				// 如已捕获前缀，则进一步校验
				if promptPrefix != "" {
					// 允许模式变化：例如 hostname(config)# 仍然包含首个提示符的主机名片段
					if !strings.Contains(trimmed, promptPrefix) {
						continue
					}
				}
				return true
			}
		}
		return false
	}

	// 辅助函数：剥离行首提示符前缀，提取可能的命令回显主体
	stripPromptPrefix := func(line string) string {
		s := sanitize(line)
		if s == "" {
			return s
		}
		// 从左到右查找最后一个提示符后缀字符的位置，并截断其后部分
		last := -1
		for _, suf := range promptSuffixes {
			idx := strings.LastIndex(s, suf)
			if idx > last {
				last = idx
			}
		}
		if last >= 0 && last+1 < len(s) {
			return strings.TrimSpace(s[last+1:])
		}
		return s
	}

	// 在开始前等待首个提示符(登录横幅后)，并捕获主机名前缀
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			stdin.Close()
			session.Close()
			return nil, ctx.Err()
		case line := <-lineCh:
			if isPrompt(line) {
				// 记录首个提示符的前缀（去掉匹配到的后缀）
				trimmed := sanitize(line)
				for _, suf := range promptSuffixes {
					if strings.HasSuffix(trimmed, suf) {
						prefix := strings.TrimSpace(trimmed[:len(trimmed)-len(suf)])
						if prefix != "" {
							promptPrefix = prefix
						}
						break
					}
				}
				goto Ready
			}
		case <-time.After(3 * time.Second):
			// 若3秒未检测到提示符，继续尝试；防止卡死
			if time.Since(start) > 10*time.Second {
				goto Ready
			}
		}
	}
Ready:
	// 停止提示符诱发器
	close(stopTrigger)
	// 清空可能残留的提示符或横幅行，避免第一条命令立即被提示符结束导致输出错位
	for {
		select {
		case <-lineCh:
			// 丢弃残留行
		default:
			goto StartCommands
		}
	}

StartCommands:
    results := make([]*CommandResult, 0, len(commands))
    // 记录上一条已发送命令，用于跳过其延迟回显（常见于网络设备在提示符后一并回显上一条命令）
    prevCmd := ""
    for _, cmd := range commands {
		// 写入命令；若写入失败，认为会话已不可用，返回错误以触发上层回退
		if _, err := stdin.Write([]byte(cmd + "\r\n")); err != nil {
			// 关闭输入并等待读取协程结束，避免资源泄露
			stdin.Close()
			select {
			case <-doneCh:
			case <-time.After(500 * time.Millisecond):
			}
			return nil, fmt.Errorf("failed to write command: %w", err)
		}

		// 收集输出直到下一个提示符
		var out strings.Builder
		sawContent := false
		// 跳过命令回显（部分设备会回显命令，且可能因换行/分页被拆分）
		echoRemain := strings.TrimSpace(cmd)
		cmdStart := time.Now()
		// 自动交互仅命中一次（每条命令），触发后不再重复执行
		autoInteractDone := false
		for {
			select {
			case <-ctx.Done():
				stdin.Close()
				session.Close()
				return results, ctx.Err()
            case line := <-lineCh:
                // 统一清洗行内容用于比较和提示符检测
                clean := sanitize(line)
                // 若出现“提示符+上一条命令”的延迟回显，直接跳过，避免写入当前命令的输出
                // 例如："hostname#terminal length 0" 在下一条命令开始时到达
                if clean != "" && prevCmd != "" {
                    candidate := stripPromptPrefix(clean)
                    pc := strings.TrimSpace(strings.ToLower(prevCmd))
                    cc := strings.TrimSpace(strings.ToLower(candidate))
                    if cc != "" {
                        if cc == pc || strings.HasPrefix(pc, cc) || strings.HasPrefix(cc, pc) {
                            // 这是上一条命令的回显或其碎片，跳过
                            continue
                        }
                    }
                }
                // 处理命令回显：剥离提示符前缀，支持被拆分到多行的回显
                if echoRemain != "" && clean != "" {
                    candidate := stripPromptPrefix(clean)
                    cmdTrim := strings.TrimSpace(cmd)
                    // 1) 常见：candidate 是 echoRemain 的前缀 → 吞掉并继续
                    if candidate != "" && strings.HasPrefix(strings.TrimSpace(echoRemain), candidate) {
                        // 规范化前缀移除（按可见文本移除）
                        er := strings.TrimSpace(echoRemain)
                        er = strings.TrimPrefix(er, candidate)
                        echoRemain = er
                        continue
                    }
					// 2) 候选包含完整命令（提示符+命令同行）→ 吞掉并结束回显
					if candidate != "" && strings.Contains(strings.ToLower(candidate), strings.ToLower(cmdTrim)) {
						echoRemain = ""
						continue
					}
					// 3) 命令包含候选（命令被拆分成若干小段）→ 吞掉并继续
					if candidate != "" && strings.Contains(strings.ToLower(cmdTrim), strings.ToLower(candidate)) {
						// 不易精确扣减，直接继续吞掉，等待后续小段补齐
						continue
					}
					// 4) 其他情况：认为回显已结束，从此行计入输出
					echoRemain = ""
				}
				// 若尚未看到内容且遇到提示符，认为是前序残留提示符，跳过
				if isPrompt(clean) && !sawContent {
					continue
				}
				// 若是提示符行（命令结束标志），不要写入输出，直接结束该命令
                if isPrompt(clean) {
                    results = append(results, &CommandResult{
                        Command:  cmd,
                        Output:   out.String(),
                        Error:    "",
                        ExitCode: 0,
                        Duration: time.Since(cmdStart),
                    })
                    goto NextCmd
                }

				// 写入正常内容
				out.WriteString(clean)
				out.WriteString("\n")
				if strings.TrimSpace(clean) != "" {
					sawContent = true
				}

				// 在执行 enable 时，遇到密码提示则自动输入密码
				// 兼容常见提示："Password:", "password:", "Enter password:"
				trimmed := clean
				lower := strings.ToLower(trimmed)
				if opts != nil && opts.EnablePassword != "" {
                    if strings.EqualFold(strings.TrimSpace(cmd), "enable") {
                        if strings.Contains(lower, "password") {
                            // 写入 enable 密码并换行（使用 CRLF 提升网络设备兼容性）
                            _, _ = stdin.Write([]byte(opts.EnablePassword + "\r\n"))
                            // 不立即结束，继续等待提示符，以确保进入特权模式
                            continue
                        }
                    }
				}

				// 自动交互：匹配提示后自动发送响应（如 more/confirm），仅命中一次
				if opts != nil && len(opts.AutoInteractions) > 0 && !autoInteractDone {
					for _, ai := range opts.AutoInteractions {
						if ai.ExpectOutput == "" || ai.AutoSend == "" {
							continue
						}
						if strings.Contains(lower, strings.ToLower(ai.ExpectOutput)) {
							_, _ = stdin.Write([]byte(ai.AutoSend + "\r\n"))
							// 命中后标记不再重复自动执行
							autoInteractDone = true
							break
						}
					}
				}
				// 提示符已在写入前处理
			case <-time.After(30 * time.Second):
				// 超时保护：将当前已读作为输出返回
				results = append(results, &CommandResult{
					Command:  cmd,
					Output:   out.String(),
					Error:    "command timeout",
					ExitCode: -1,
					Duration: time.Since(cmdStart),
				})
				goto NextCmd
			}
        }
    NextCmd:
        // 记录上一条命令，供下一条命令跳过其延迟回显
        prevCmd = cmd
        // 命令间隔控制（避免过快触发设备限流或分页）
        if opts != nil && opts.CommandIntervalMS > 0 {
            time.Sleep(time.Duration(opts.CommandIntervalMS) * time.Millisecond)
        }
        // 继续处理下一条命令
    }

	// 优雅关闭交互通道：按配置的退出命令序列依次尝试
	exitSeq := []string{"exit", "quit"}
	if opts != nil && len(opts.ExitCommands) > 0 {
		exitSeq = opts.ExitCommands
	}
	for _, ec := range exitSeq {
		_, _ = stdin.Write([]byte(ec + "\r\n"))
		time.Sleep(150 * time.Millisecond)
	}

	stdin.Close()
	// 等待读取协程结束
	select {
	case <-doneCh:
	case <-time.After(1 * time.Second):
	}

	return results, nil
}

// Close 关闭SSH连接
func (c *Client) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 关闭所有会话
	for _, session := range c.sessions {
		session.Close()
	}
	c.sessions = make(map[string]*ssh.Session)

	// 关闭连接
	if c.connection != nil {
		err := c.connection.Close()
		c.connection = nil
		return err
	}

	return nil
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	c.mutex.RLock()
	conn := c.connection
	c.mutex.RUnlock()
	if conn == nil {
		return false
	}
	// 轻量级健康检查：发送 keepalive 请求而不创建会话，避免触发设备的会话数量限制
	// 若底层连接已断开，SendRequest 会返回错误；否则认为连接仍可用
	_, _, err := conn.SendRequest("keepalive@openssh.com", false, nil)
	return err == nil
}

// keepAlive 保持连接活跃
func (c *Client) keepAlive(ctx context.Context) {
	if c.config.KeepAlive <= 0 {
		return
	}

	ticker := time.NewTicker(c.config.KeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !c.IsConnected() {
				return
			}

			// 发送保活请求（不等待回复，避免不支持该请求的设备导致错误）
			_, _, err := c.connection.SendRequest("keepalive@openssh.com", false, nil)
			if err != nil {
				// 连接可能已断开，主动关闭并置空以便池清理
				c.mutex.Lock()
				if c.connection != nil {
					_ = c.connection.Close()
					c.connection = nil
				}
				c.mutex.Unlock()
				return
			}
		}
	}
}

// GetConnectionStats 获取连接统计信息
func (c *Client) GetConnectionStats() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	stats := map[string]interface{}{
		"connected":     c.connection != nil,
		"session_count": len(c.sessions),
	}

	return stats
}
