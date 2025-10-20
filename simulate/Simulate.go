package simulate

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"

	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// Simulate.yaml 配置结构
// 注意：根据需求使用 prompt_suffixe/enable_mode_suffixe 键名（带 e）
type Config struct {
	Namespace  map[string]NamespaceConfig  `mapstructure:"namespace"`
	DeviceType map[string]DeviceTypeConfig `mapstructure:"device_type"`
	DeviceName map[string]DeviceNameConfig `mapstructure:"device_name"`
}

type NamespaceConfig struct {
	Port        int `mapstructure:"port"`
	IdleSeconds int `mapstructure:"idle_seconds"`
	MaxConn     int `mapstructure:"max_conn"`
}

type DeviceTypeConfig struct {
	PromptSuffix       string `mapstructure:"prompt_suffixe"`
	EnableModeRequired bool   `mapstructure:"enable_mode_required"`
	EnableModeSuffix   string `mapstructure:"enable_mode_suffixe"`
}

type DeviceNameConfig struct {
	DeviceType string `mapstructure:"device_type"`
}

// Manager 管理多个 namespace 的 SSH 模拟服务
// 每个 namespace 在独立端口运行，互不影响
// 通过用户名选择设备名称，匹配到设备类型与提示符

type Manager struct {
	cfg       *Config
	nsServers map[string]*namespaceServer
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
}

type namespaceServer struct {
	nsName   string
	cfg      NamespaceConfig
	simCfg   *Config
	listener net.Listener
	hostKey  ssh.Signer
	active   int
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// LoadConfig 读取 simulate/simulate.yaml
func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read simulate config: %w", err)
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal simulate config: %w", err)
	}
	return &cfg, nil
}

// EnsureDirs 启动时根据 namespace 与 device_name 自动创建目录结构
// simulate/namespace/<ns>/<device_name>
func EnsureDirs(simCfg *Config) error {
	base := filepath.Join("simulate", "namespace")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fmt.Errorf("failed to create base namespace directory: %w", err)
	}
	for ns := range simCfg.Namespace {
		for dev := range simCfg.DeviceName {
			dir := filepath.Join(base, ns, dev)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("failed to create dir %s: %w", dir, err)
			}
		}
	}
	return nil
}

// Start 启动所有 namespace 的 SSH 模拟服务
func Start(simCfg *Config) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:       simCfg,
		nsServers: make(map[string]*namespaceServer),
		ctx:       ctx,
		cancel:    cancel,
	}

	// 准备目录结构
	if err := EnsureDirs(simCfg); err != nil {
		logger.Error("Simulate: ensure dirs failed", "error", err)
		return nil, err
	}

	// 按 namespace 启动 SSH server
	for ns, nsCfg := range simCfg.Namespace {
		srv, err := newNamespaceServer(ns, nsCfg, simCfg)
		if err != nil {
			logger.Error("Simulate: init namespace server failed", "namespace", ns, "error", err)
			continue
		}
		if err := srv.start(); err != nil {
			logger.Error("Simulate: start namespace server failed", "namespace", ns, "port", nsCfg.Port, "error", err)
			continue
		}
		m.nsServers[ns] = srv
		logger.Info("Simulate: namespace server started", "namespace", ns, "port", nsCfg.Port)
	}

	return m, nil
}

// Stop 停止所有模拟服务
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	for ns, srv := range m.nsServers {
		srv.stop()
		logger.Info("Simulate: namespace server stopped", "namespace", ns)
	}
}

func newNamespaceServer(nsName string, nsCfg NamespaceConfig, simCfg *Config) (*namespaceServer, error) {
	// 生成临时 host key（RSA 2048）
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate host key: %w", err)
	}
	privDER := x509MarshalPKCS1PrivateKey(key)
	signer, err := ssh.ParsePrivateKey(privDER)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host key: %w", err)
	}

	return &namespaceServer{
		nsName:  nsName,
		cfg:     nsCfg,
		simCfg:  simCfg,
		hostKey: signer,
	}, nil
}

// 因为标准库没有直接暴露 x509 marshal，这里手动 marshal 成 PEM 然后解析出 DER
// 但 ssh.ParsePrivateKey 接受的是 PEM 或 DER；我们直接返回 PEM 编码的字节即可
func x509MarshalPKCS1PrivateKey(key *rsa.PrivateKey) []byte {
	privDER := x509.MarshalPKCS1PrivateKey(key)
	blk := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER}
	return pem.EncodeToMemory(blk)
}

func (s *namespaceServer) start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return err
	}
	s.listener = ln

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					logger.Warn("Simulate: accept temporary error", "error", err)
					time.Sleep(200 * time.Millisecond)
					continue
				}
				// listener closed
				return
			}
			// 并发限制
			s.mu.Lock()
			if s.cfg.MaxConn > 0 && s.active >= s.cfg.MaxConn {
				s.mu.Unlock()
				_ = conn.Close()
				logger.Warn("Simulate: reject connection, max_conn exceeded", "namespace", s.nsName)
				continue
			}
			s.active++
			s.mu.Unlock()

			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				s.handleConn(c)
				s.mu.Lock()
				s.active--
				s.mu.Unlock()
			}(conn)
		}
	}()

	return nil
}

func (s *namespaceServer) stop() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.wg.Wait()
}

func (s *namespaceServer) handleConn(nc net.Conn) {
	// 构造 SSH ServerConfig：允许任意用户名（作为设备名），密码统一为 nova
	srvCfg := &ssh.ServerConfig{
		NoClientAuth: false,
		PasswordCallback: func(connMetadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			user := strings.TrimSpace(connMetadata.User())
			_ = user // 用户名用作设备名，不参与认证判断
			pass := strings.TrimSpace(string(password))
			if pass == "nova" {
				return nil, nil
			}
			return nil, fmt.Errorf("access denied")
		},
		KeyboardInteractiveCallback: func(connMetadata ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			// 兼容部分客户端默认使用 keyboard-interactive 的情况
			// 向客户端发送一次密码提示，并校验返回的答案
			answers, err := challenge(connMetadata.User(), "Authentication", []string{"Password:"}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) > 0 && strings.TrimSpace(answers[0]) == "nova" {
				return nil, nil
			}
			return nil, fmt.Errorf("access denied")
		},
	}
	srvCfg.AddHostKey(s.hostKey)

	// 完成握手
	conn, chans, reqs, err := ssh.NewServerConn(nc, srvCfg)
	if err != nil {
		logger.Error("Simulate: SSH handshake failed", "namespace", s.nsName, "error", err)
		_ = nc.Close()
		return
	}
	defer conn.Close()

	// 丢弃全局请求
	go ssh.DiscardRequests(reqs)

	// 处理会话通道
	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			logger.Error("Simulate: channel accept failed", "error", err)
			continue
		}

		// 设备名称使用用户名
		deviceName := conn.User()
		devType := s.resolveDeviceType(deviceName)
		promptSuffix := devType.PromptSuffix
		enableRequired := devType.EnableModeRequired
		enableSuffix := devType.EnableModeSuffix

		// 处理请求（pty-req / shell / exec）
		go s.handleSession(channel, requests, deviceName, promptSuffix, enableRequired, enableSuffix)
	}
}

func (s *namespaceServer) resolveDeviceType(deviceName string) DeviceTypeConfig {
	// 根据 device_name 映射到设备类型，否则返回一个通用默认
	if dn, ok := s.simCfg.DeviceName[deviceName]; ok {
		if dt, ok := s.simCfg.DeviceType[dn.DeviceType]; ok {
			return dt
		}
	}
	// 默认提示符后缀">"
	return DeviceTypeConfig{PromptSuffix: ">", EnableModeRequired: false, EnableModeSuffix: "#"}
}

func (s *namespaceServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request, deviceName, promptSuffix string, enableRequired bool, enableSuffix string) {
	defer channel.Close()

	// 跟踪 PTY 是否已就绪
	var ptyReady bool

	// 处理请求类型
	for req := range requests {
		switch req.Type {
		case "pty-req":
			ptyReady = true
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			// 进入交互式 shell
			s.runInteractiveShell(channel, deviceName, promptSuffix, enableRequired, enableSuffix)
			return
		case "exec":
			// 执行单条命令并返回结果
			cmd := string(req.Payload)
			// OpenSSH 发送的 payload 包含命令长度等结构；简单处理：提取最后一个可见字符串
			cmd = extractCommandFromPayload(cmd)
			out := s.loadCommandOutput(s.nsName, deviceName, cmd)
			if out == "" {
				out = "unsupportted command\r\n"
			}
			channel.Write([]byte(out))
			if ptyReady {
				channel.Write([]byte(fmt.Sprintf("%s%s\r\n", deviceName, promptSuffix)))
			}
			req.Reply(true, nil)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *namespaceServer) runInteractiveShell(channel ssh.Channel, deviceName, promptSuffix string, enableRequired bool, enableSuffix string) {
	// 初始提示符
	currentSuffix := promptSuffix
	printPrompt := func() {
		channel.Write([]byte(fmt.Sprintf("%s%s\r\n", deviceName, currentSuffix)))
	}
	printPrompt()

	reader := bufio.NewReader(channel)

	idle := s.cfg.IdleSeconds
	var idleTimer *time.Timer
	if idle > 0 {
		idleTimer = time.NewTimer(time.Duration(idle) * time.Second)
		defer idleTimer.Stop()
	}

	for {
		// 若设置了 idle 超时，则检查
		if idleTimer != nil {
			select {
			case <-idleTimer.C:
				channel.Write([]byte("\r\nSession closed due to idle timeout.\r\n"))
				return
			default:
			}
		}

		// 读取一行命令
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			// 某些客户端用 CR 结束
			if errorsIs(err, io.ErrUnexpectedEOF) {
				// 尝试读取剩余
				if line == "" {
					return
				}
			} else {
				return
			}
		}

		cmd := strings.TrimSpace(cleanNewlines(line))
		if cmd == "" {
			// 1) 无命令输入，显示新的一行
			channel.Write([]byte("\r\n"))
			printPrompt()
			continue
		}

		// 重置 idle timer
		if idleTimer != nil {
			idleTimer.Reset(time.Duration(idle) * time.Second)
		}

		// 处理退出
		if equalAny(cmd, "exit", "quit") {
			channel.Write([]byte("\r\n"))
			return
		}

		// 处理 enable：统一要求提权密码为 nova
		if enableRequired && strings.EqualFold(cmd, "enable") {
			channel.Write([]byte("Password:\r\n"))
			pwd, _ := reader.ReadString('\n')
			if strings.TrimSpace(cleanNewlines(pwd)) != "nova" {
				channel.Write([]byte("Bad secrets\r\n"))
				printPrompt()
				continue
			}
			currentSuffix = chooseNonEmpty(enableSuffix, "#")
			printPrompt()
			continue
		}

		// 加载模拟命令输出
		out := s.loadCommandOutput(s.nsName, deviceName, cmd)
		if out == "" {
			// 3) 未匹配：显示固定文案
			out = "unsupportted command\r\n"
		}
		// 2) 匹配：显示 txt 文件内容（已按 CRLF 标准化）
		channel.Write([]byte(out))
		printPrompt()
	}
}

func (s *namespaceServer) loadCommandOutput(ns, deviceName, cmd string) string {
	base := filepath.Join("simulate", "namespace", ns, deviceName)
	// 尝试原命令名称
	p1 := filepath.Join(base, fmt.Sprintf("%s.txt", cmd))
	if bs, err := os.ReadFile(p1); err == nil {
		return ensureCRLF(string(bs))
	}
	// 尝试替换空格为下划线
	normalized := strings.ReplaceAll(cmd, " ", "_")
	p2 := filepath.Join(base, fmt.Sprintf("%s.txt", normalized))
	if bs, err := os.ReadFile(p2); err == nil {
		return ensureCRLF(string(bs))
	}
	return ""
}

// extractCommandFromPayload 尝试从 exec payload 中提取命令字符串
func extractCommandFromPayload(payload string) string {
	// 简化处理：去掉不可见字符，取最后一段
	s := strings.TrimSpace(strings.ReplaceAll(payload, "\x00", ""))
	if s == "" {
		return s
	}
	// OpenSSH 格式往往包含 "\x00\x00\x00..." 前缀，粗略剥离
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	return strings.TrimSpace(strings.Join(parts, " "))
}

func ensureCRLF(s string) string {
	// 将 \n 规范为 \r\n
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return s
}

func cleanNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func errorsIs(err error, target error) bool {
	// 避免额外依赖，提供最小 is
	return fmt.Sprintf("%v", err) == fmt.Sprintf("%v", target)
}

func equalAny(s string, opts ...string) bool {
	for _, o := range opts {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(o)) {
			return true
		}
	}
	return false
}

func chooseNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
