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
	}

	// 设置认证方式
	if info.Password != "" {
		sshConfig.Auth = []ssh.AuthMethod{
			ssh.Password(info.Password),
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

// ExecuteCommand 执行单个命令
func (c *Client) ExecuteCommand(ctx context.Context, command string) (*CommandResult, error) {
	if c.connection == nil {
		return nil, fmt.Errorf("SSH connection not established")
	}

	startTime := time.Now()
	result := &CommandResult{
		Command: command,
	}

	// 创建会话
	session, err := c.connection.NewSession()
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

	// 创建会话
	session, err := c.connection.NewSession()
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
            if err := session.RequestPty(term, 80, 24, modes); err == nil {
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

    session, err := c.connection.NewSession()
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
            if err := session.RequestPty(term, 80, 24, modes); err == nil {
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
                // 统一换行符，兼容仅使用 CR 的设备输出
                s = strings.ReplaceAll(s, "\r", "\n")
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
                // 统一换行符，兼容仅使用 CR 的设备输出
                s = strings.ReplaceAll(s, "\r", "\n")
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

    // 辅助函数：判断行是否是提示符
    isPrompt := func(line string) bool {
        trimmed := strings.TrimSpace(line)
        if trimmed == "" { return false }
        // 例如 "<outside-rt-01>"、"Router#"、"Switch>"
        for _, suf := range promptSuffixes {
            if strings.HasSuffix(trimmed, suf) {
                return true
            }
        }
        return false
    }

    // 在开始前等待首个提示符(登录横幅后)
    start := time.Now()
    for {
        select {
        case <-ctx.Done():
            stdin.Close(); session.Close()
            return nil, ctx.Err()
        case line := <-lineCh:
            if isPrompt(line) {
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

    results := make([]*CommandResult, 0, len(commands))
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
        cmdStart := time.Now()
        for {
            select {
            case <-ctx.Done():
                stdin.Close(); session.Close()
                return results, ctx.Err()
            case line := <-lineCh:
                // 丢弃包含命令回显的行(设备通常回显命令)，但保留整体输出
                out.WriteString(line)
                out.WriteString("\n")

                // 在执行 enable 时，遇到密码提示则自动输入密码
                // 兼容常见提示："Password:", "password:", "Enter password:"
                trimmed := strings.TrimSpace(line)
                lower := strings.ToLower(trimmed)
                if opts != nil && opts.EnablePassword != "" {
                    if strings.EqualFold(strings.TrimSpace(cmd), "enable") {
                        if strings.Contains(lower, "password") {
                            // 写入 enable 密码并换行
                            _, _ = stdin.Write([]byte(opts.EnablePassword + "\n"))
                            // 不立即结束，继续等待提示符，以确保进入特权模式
                            continue
                        }
                    }
                }
                if isPrompt(line) {
                    // 到达提示符，认为该命令结束
                    results = append(results, &CommandResult{
                        Command:  cmd,
                        Output:   out.String(),
                        Error:    "",
                        ExitCode: 0,
                        Duration: time.Since(cmdStart),
                    })
                    goto NextCmd
                }
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
    // 进行轻量级探测：尝试创建会话以确认连接仍有效
    // 某些情况下远端断开但本地仍保留指针，NewSession 会返回 EOF
    sess, err := conn.NewSession()
    if err != nil {
        return false
    }
    sess.Close()
    return true
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