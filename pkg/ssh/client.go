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

	// 设置终端模式
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // 禁用回显
		ssh.TTY_OP_ISPEED: 14400, // 输入速度
		ssh.TTY_OP_OSPEED: 14400, // 输出速度
	}

	err = session.RequestPty("xterm", 80, 40, modes)
	if err != nil {
		result.Error = fmt.Sprintf("failed to request pty: %v", err)
		result.ExitCode = -1
		return result, err
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
			stdin.Write([]byte(response + "\n"))
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
	defer c.mutex.RUnlock()
	return c.connection != nil
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
			
			// 发送保活请求
			_, _, err := c.connection.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				// 连接可能已断开
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