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
	"regexp"

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

// Reload 热更新模拟配置：按命名空间增删改并重载端口
func (m *Manager) Reload(newCfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if newCfg == nil {
		return fmt.Errorf("nil simulate config")
	}

	// 确保新目录结构存在
	if err := EnsureDirs(newCfg); err != nil {
		logger.Warn("Simulate: ensure dirs on reload failed", "error", err)
	}

	// 1) 停止并移除在新配置中不存在的命名空间
	for ns, srv := range m.nsServers {
		if _, ok := newCfg.Namespace[ns]; !ok {
			srv.stop()
			delete(m.nsServers, ns)
			logger.Info("Simulate: namespace removed", "namespace", ns)
		}
	}

	// 2) 新增或更新现有命名空间（端口变化则重启）
	for ns, nsCfg := range newCfg.Namespace {
		if srv, ok := m.nsServers[ns]; ok {
			portChanged := srv.cfg.Port != nsCfg.Port
			// 更新运行时配置
			srv.cfg = nsCfg
			srv.simCfg = newCfg
			if portChanged {
				srv.stop()
				if err := srv.start(); err != nil {
					logger.Warn("Simulate: namespace restart failed", "namespace", ns, "port", nsCfg.Port, "error", err)
				} else {
					logger.Info("Simulate: namespace restarted", "namespace", ns, "port", nsCfg.Port)
				}
			} else {
				logger.Info("Simulate: namespace updated", "namespace", ns, "port", nsCfg.Port)
			}
			continue
		}
		// 新增命名空间
		srv, err := newNamespaceServer(ns, nsCfg, newCfg)
		if err != nil {
			logger.Warn("Simulate: init new namespace failed", "namespace", ns, "error", err)
			continue
		}
		if err := srv.start(); err != nil {
			logger.Warn("Simulate: start new namespace failed", "namespace", ns, "port", nsCfg.Port, "error", err)
			continue
		}
		m.nsServers[ns] = srv
		logger.Info("Simulate: namespace added", "namespace", ns, "port", nsCfg.Port)
	}

	m.cfg = newCfg
	return nil
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
	// 改为按 namespace 持久化 host key，避免客户端指纹频繁变化
	signer, err := loadOrCreateHostKey(nsName)
	if err != nil {
		return nil, fmt.Errorf("failed to init host key: %w", err)
	}

	logger.Debug("Simulate: new namespace server", "namespace", nsName, "port", nsCfg.Port)
	return &namespaceServer{
		nsName:  nsName,
		cfg:     nsCfg,
		simCfg:  simCfg,
		hostKey: signer,
	}, nil
}

// 新增：按 namespace 加载或生成持久化的 host key（RSA 2048）
func loadOrCreateHostKey(_ string) (ssh.Signer, error) {
	// 全局共享 host key 路径：simulate/_hostkey_rsa.pem
	keyDir := filepath.Join("simulate")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to ensure simulate dir: %w", err)
	}
	keyPath := filepath.Join(keyDir, "_hostkey_rsa.pem")

	// 优先尝试加载全局密钥
	if bs, err := os.ReadFile(keyPath); err == nil {
		signer, err := ssh.ParsePrivateKey(bs)
		if err == nil {
			logger.Debug("Simulate: global host key loaded", "file", keyPath)
			return signer, nil
		}
		logger.Warn("Simulate: global host key parse failed, regenerating", "error", err)
	}

	// 迁移兼容：若全局密钥不存在，尝试从任何 namespace 旧位置复用
	baseNs := filepath.Join("simulate", "namespace")
	if entries, err := os.ReadDir(baseNs); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			old := filepath.Join(baseNs, e.Name(), "_hostkey_rsa.pem")
			if bs, err := os.ReadFile(old); err == nil {
				if err := os.WriteFile(keyPath, bs, 0o600); err == nil {
					signer, perr := ssh.ParsePrivateKey(bs)
					if perr == nil {
						logger.Info("Simulate: migrated host key from namespace", "namespace", e.Name(), "file", keyPath)
						return signer, nil
					}
				}
			}
		}
	}

	// 不存在或迁移失败则生成新密钥并持久化
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate rsa key: %w", err)
	}
	bs := x509.MarshalPKCS1PrivateKey(key)
	pemBlock := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: bs}
	if werr := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600); werr != nil {
		return nil, fmt.Errorf("failed to persist rsa key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem.EncodeToMemory(pemBlock))
	if err != nil {
		return nil, fmt.Errorf("failed to parse persisted rsa key: %w", err)
	}
	logger.Info("Simulate: new host key generated", "file", keyPath)
	return signer, nil
}

func (s *namespaceServer) start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return err
	}
	s.listener = ln
	logger.Debug("Simulate: listener started", "namespace", s.nsName, "port", s.cfg.Port)

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
			logger.Debug("Simulate: accept connection", "namespace", s.nsName, "remote", conn.RemoteAddr().String())
			// 并发限制
			s.mu.Lock()
			if s.cfg.MaxConn > 0 && s.active >= s.cfg.MaxConn {
				s.mu.Unlock()
				_ = conn.Close()
				logger.Warn("Simulate: reject connection, max_conn exceeded", "namespace", s.nsName)
				logger.Debug("Simulate: active", "active", s.active)
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
	logger.Debug("Simulate: handshake start", "namespace", s.nsName, "remote", nc.RemoteAddr().String())
	srvCfg := &ssh.ServerConfig{
		NoClientAuth: false,
		PasswordCallback: func(connMetadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			user := strings.TrimSpace(connMetadata.User())
			pass := strings.TrimSpace(string(password))
			logger.Debug("Simulate: auth try (password)", "user", user)
			if pass == "nova" {
				logger.Debug("Simulate: auth success (password)", "user", user)
				return nil, nil
			}
			logger.Debug("Simulate: auth failed (password)", "user", user)
			return nil, fmt.Errorf("access denied")
		},
		KeyboardInteractiveCallback: func(connMetadata ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			// 兼容部分客户端默认使用 keyboard-interactive 的情况
			logger.Debug("Simulate: auth try (keyboard-interactive)", "user", connMetadata.User())
			answers, err := challenge(connMetadata.User(), "Authentication", []string{"Password:"}, []bool{false})
			if err != nil {
				logger.Debug("Simulate: auth failed (ki challenge)", "error", err)
				return nil, err
			}
			if len(answers) > 0 && strings.TrimSpace(answers[0]) == "nova" {
				logger.Debug("Simulate: auth success (keyboard-interactive)", "user", connMetadata.User())
				return nil, nil
			}
			logger.Debug("Simulate: auth failed (keyboard-interactive)", "user", connMetadata.User())
			return nil, fmt.Errorf("access denied")
		},
	}
	srvCfg.AddHostKey(s.hostKey)

	// 完成握手
	conn, chans, reqs, err := ssh.NewServerConn(nc, srvCfg)
	if err != nil {
		logger.Error("Simulate: SSH handshake failed", "namespace", s.nsName, "remote", nc.RemoteAddr().String(), "error", err)
		_ = nc.Close()
		return
	}
	logger.Debug("Simulate: handshake success", "namespace", s.nsName, "user", conn.User(), "remote", nc.RemoteAddr().String(), "client", string(conn.ClientVersion()))
	defer conn.Close()

	// 丢弃全局请求
	go ssh.DiscardRequests(reqs)

	// 处理会话通道
	for ch := range chans {
		logger.Debug("Simulate: channel type", "type", ch.ChannelType())
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			logger.Error("Simulate: channel accept failed", "error", err)
			continue
		}
		logger.Debug("Simulate: session channel accepted", "namespace", s.nsName, "user", conn.User())

		// 设备名称使用用户名
		deviceName := conn.User()
		devType := s.resolveDeviceType(deviceName)
		promptSuffix := devType.PromptSuffix
		enableRequired := devType.EnableModeRequired
		enableSuffix := devType.EnableModeSuffix

		logger.Debug("Simulate: device resolved", "device", deviceName, "prompt_suffix", promptSuffix, "enable_required", enableRequired, "enable_suffix", enableSuffix)
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
			logger.Debug("Simulate: pty-req ok", "device", deviceName)
		case "shell":
			req.Reply(true, nil)
			logger.Debug("Simulate: shell start", "device", deviceName)
			// 进入交互式 shell
			s.runInteractiveShell(channel, deviceName, promptSuffix, enableRequired, enableSuffix)
			return
		case "exec":
			// 执行单条命令并返回结果
			cmd := string(req.Payload)
			// OpenSSH 发送的 payload 包含命令长度等结构；简单处理：提取最后一个可见字符串
			cmd = extractCommandFromPayload(cmd)
			logger.Debug("Simulate: exec cmd", "device", deviceName, "cmd", cmd)
			out := s.loadCommandOutput(s.nsName, deviceName, cmd)
			if out == "" {
				logger.Debug("Simulate: exec unmatched", "cmd", cmd)
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
			logger.Debug("Simulate: unknown request", "type", req.Type)
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
	logger.Debug("Simulate: prompt printed", "device", deviceName, "suffix", currentSuffix)

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
				logger.Debug("Simulate: session idle timeout", "device", deviceName)
				return
			default:
			}
		}

		// 读取一行命令
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				logger.Debug("Simulate: session EOF", "device", deviceName)
				return
			}
			// 某些客户端用 CR 结束
			if errorsIs(err, io.ErrUnexpectedEOF) {
				// 尝试读取剩余
				if line == "" {
					logger.Debug("Simulate: session unexpected EOF with empty line", "device", deviceName)
					return
				}
			} else {
				logger.Debug("Simulate: session read error", "device", deviceName, "error", err)
				return
			}
		}

		cmd := strings.TrimSpace(cleanNewlines(line))
		logger.Debug("Simulate: input", "device", deviceName, "cmd", cmd)
		if cmd == "" {
			// 1) 无命令输入，显示新的一行
			channel.Write([]byte("\r\n"))
			printPrompt()
			continue
		}

		// 重置 idle timer
		if idleTimer != nil {
			idleTimer.Reset(time.Duration(idle) * time.Second)
			logger.Debug("Simulate: idle timer reset", "device", deviceName)
		}

		// 处理退出
		if equalAny(cmd, "exit", "quit") {
			channel.Write([]byte("\r\n"))
			logger.Debug("Simulate: session exit", "device", deviceName)
			return
		}

		// 处理 enable：统一要求提权密码为 nova
		if enableRequired && strings.EqualFold(cmd, "enable") {
			logger.Debug("Simulate: enable requested", "device", deviceName)
			channel.Write([]byte("Password:\r\n"))
			pwd, _ := reader.ReadString('\n')
			if strings.TrimSpace(cleanNewlines(pwd)) != "nova" {
				channel.Write([]byte("Bad secrets\r\n"))
				logger.Debug("Simulate: enable failed", "device", deviceName)
				printPrompt()
				continue
			}
			currentSuffix = chooseNonEmpty(enableSuffix, "#")
			logger.Debug("Simulate: enable success", "device", deviceName, "suffix", currentSuffix)
			printPrompt()
			continue
		}

		// 加载模拟命令输出
		out := s.loadCommandOutput(s.nsName, deviceName, cmd)
		if out == "" {
			// 3) 未匹配：显示固定文案
			logger.Debug("Simulate: command unmatched", "device", deviceName, "cmd", cmd)
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
		logger.Debug("Simulate: load out (direct)", "device", deviceName, "cmd", cmd, "file", p1)
		return ensureCRLF(string(bs))
	}
	// 尝试替换空格为下划线
	normalized := strings.ReplaceAll(cmd, " ", "_")
	p2 := filepath.Join(base, fmt.Sprintf("%s.txt", normalized))
	if bs, err := os.ReadFile(p2); err == nil {
		logger.Debug("Simulate: load out (normalized)", "device", deviceName, "cmd", cmd, "file", p2)
		return ensureCRLF(string(bs))
	}
	// 未直接命中：进入模糊匹配
	cands, fileMap := s.listSupportedCommands(base)
	if len(cands) == 0 {
		logger.Debug("Simulate: no supported commands listed", "device", deviceName)
		return "unsupportted command\r\n"
	}
	matches := fuzzyMatchCommands(cmd, cands)
	// 追加：按词前缀的正则匹配并与原结果合并
	prefixMatches := prefixWordMatchCommands(cmd, cands)
	logger.Debug("Simulate: match summary", "cmd", cmd, "fuzzy_count", len(matches), "prefix_count", len(prefixMatches))
	if len(matches) == 0 && len(prefixMatches) > 0 {
		matches = prefixMatches
	} else if len(prefixMatches) > 0 {
		uniq := make(map[string]struct{}, len(matches)+len(prefixMatches))
		merged := make([]string, 0, len(matches)+len(prefixMatches))
		for _, m := range matches { if _, ok := uniq[m]; !ok { uniq[m] = struct{}{}; merged = append(merged, m) } }
		for _, m := range prefixMatches { if _, ok := uniq[m]; !ok { uniq[m] = struct{}{}; merged = append(merged, m) } }
		matches = merged
	}
	if len(matches) == 0 {
		return "unsupportted command\r\n"
	}
	if len(matches) == 1 {
		// 单一匹配：读取对应文件
		chosen := matches[0]
		if fp, ok := fileMap[chosen]; ok {
			if bs, err := os.ReadFile(fp); err == nil {
				logger.Debug("Simulate: load out (fuzzy)", "device", deviceName, "cmd", cmd, "file", fp)
				return ensureCRLF(string(bs))
			}
		}
		// 回退：按候选名尝试 direct/normalized
		p3 := filepath.Join(base, fmt.Sprintf("%s.txt", chosen))
		if bs, err := os.ReadFile(p3); err == nil {
			return ensureCRLF(string(bs))
		}
		p4 := filepath.Join(base, fmt.Sprintf("%s.txt", strings.ReplaceAll(chosen, " ", "_")))
		if bs, err := os.ReadFile(p4); err == nil {
			return ensureCRLF(string(bs))
		}
		return "unsupportted command\r\n"
	}
	// 多个匹配：输出建议列表
	var b strings.Builder
	b.WriteString("which command do you mean:\r\n")
	for _, m := range matches {
		b.WriteString("-- ")
		b.WriteString(m)
		b.WriteString("\r\n")
	}
	return b.String()
}

// 列出支持命令集合：优先 supported_commands.txt，回退为扫描 *.txt 文件
func (s *namespaceServer) listSupportedCommands(base string) ([]string, map[string]string) {
	cands := make([]string, 0, 64)
	fileMap := make(map[string]string, 64)
	// 扫描目录中的 .txt 文件
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			if e.IsDir() { continue }
			name := e.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".txt") { continue }
			if strings.EqualFold(name, "supported_commands.txt") { continue }
			stem := strings.TrimSuffix(name, ".txt")
			canon := strings.ReplaceAll(stem, "_", " ")
			fileMap[canon] = filepath.Join(base, name)
			cands = append(cands, canon)
		}
	}
	// 若存在 supported_commands.txt，合并其中条目（可列出但文件可选存在）
	listPath := filepath.Join(base, "supported_commands.txt")
	if bs, err := os.ReadFile(listPath); err == nil {
		for _, ln := range strings.Split(string(bs), "\n") {
			ln = strings.TrimSpace(strings.TrimRight(strings.ReplaceAll(ln, "\r", ""), "\n"))
			if ln == "" || strings.HasPrefix(ln, "#") { continue }
			// 若已存在于扫描结果则跳过；否则添加候选并尝试推导文件名映射
			exists := false
			for _, c := range cands { if strings.EqualFold(c, ln) { exists = true; break } }
			if !exists {
				cands = append(cands, ln)
				// 推导规范文件路径（可能不存在，加载时再兜底）
				norm := strings.ReplaceAll(ln, " ", "_")
				fp := filepath.Join(base, fmt.Sprintf("%s.txt", norm))
				if _, err := os.Stat(fp); err == nil { fileMap[ln] = fp }
			}
		}
	}
	return cands, fileMap
}

// 正则模糊匹配（大小写不敏感；空格/下划线视为任意空白；允许包含匹配）
func fuzzyMatchCommands(input string, cands []string) []string {
	in := strings.TrimSpace(input)
	if in == "" { return nil }
	// 构造正则：转义元字符，空格/下划线替换为 \s+
	esc := regexp.QuoteMeta(in)
	esc = strings.ReplaceAll(esc, "_", "\\s+")
	esc = strings.ReplaceAll(esc, " ", "\\s+")
	pattern := "(?i).*" + esc + ".*"
	re, err := regexp.Compile(pattern)
	res := make([]string, 0, len(cands))
	if err == nil {
		for _, c := range cands {
			if re.MatchString(c) {
				res = append(res, c)
			}
		}
		return res
	}
	// 回退：大小写不敏感的包含匹配
	low := strings.ToLower(in)
	for _, c := range cands {
		if strings.Contains(strings.ToLower(c), low) {
			res = append(res, c)
		}
	}
	return res
}

// 新增：按词前缀的正则匹配（大小写不敏感；从命令首词开始顺序匹配）
func prefixWordMatchCommands(input string, cands []string) []string {
	in := strings.TrimSpace(strings.ReplaceAll(input, "_", " "))
	if in == "" { return nil }
	parts := strings.Fields(strings.ToLower(in))
	res := make([]string, 0, len(cands))
	for _, c := range cands {
		cparts := strings.Fields(strings.ToLower(strings.ReplaceAll(c, "_", " ")))
		if len(parts) > len(cparts) { continue }
		ok := true
		for i := range parts {
			esc := regexp.QuoteMeta(parts[i])
			pat := "(?i)^" + esc + ".*"
			re, err := regexp.Compile(pat)
			if err != nil || !re.MatchString(cparts[i]) {
				ok = false
				break
			}
		}
		if ok { res = append(res, c) }
	}
	return res
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
	// 保证每个输出片段以 CRLF 结尾，避免客户端读行跳过
	if !strings.HasSuffix(s, "\r\n") {
		if strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, "\r\n") {
			return strings.ReplaceAll(s, "\n", "\r\n")
		}
		return s + "\r\n"
	}
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
