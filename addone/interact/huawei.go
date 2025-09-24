package interact

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// HuaweiInteractor 华为设备交互器
type HuaweiInteractor struct {
	Name        string
	Version     string
	Description string
	Vendor      string
}

// InteractionStep 交互步骤
type InteractionStep struct {
	Command     string        `json:"command"`
	Expect      string        `json:"expect"`
	Response    string        `json:"response"`
	Timeout     time.Duration `json:"timeout"`
	Optional    bool          `json:"optional"`
	Description string        `json:"description"`
}

// InteractionSession 交互会话
type InteractionSession struct {
	DeviceType string             `json:"device_type"`
	Steps      []*InteractionStep `json:"steps"`
	Context    map[string]string  `json:"context"`
}

// NewHuaweiInteractor 创建华为交互器实例
func NewHuaweiInteractor() *HuaweiInteractor {
	return &HuaweiInteractor{
		Name:        "huawei-interactor",
		Version:     "1.0.0",
		Description: "华为设备专用交互插件，处理设备特定的交互逻辑",
		Vendor:      "Huawei",
	}
}

// GetInfo 获取插件信息
func (h *HuaweiInteractor) GetInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":        h.Name,
		"version":     h.Version,
		"description": h.Description,
		"vendor":      h.Vendor,
		"capabilities": []string{
			"login_automation",
			"privilege_escalation",
			"configuration_mode",
			"save_configuration",
			"error_handling",
		},
	}
}

// CreateLoginSession 创建登录会话
func (h *HuaweiInteractor) CreateLoginSession(deviceType string) *InteractionSession {
	session := &InteractionSession{
		DeviceType: deviceType,
		Context:    make(map[string]string),
		Steps: []*InteractionStep{
			{
				Command:     "",
				Expect:      `(?i)(username|login):\s*$`,
				Response:    "{username}",
				Timeout:     30 * time.Second,
				Description: "等待用户名提示",
			},
			{
				Command:     "{username}",
				Expect:      `(?i)password:\s*$`,
				Response:    "{password}",
				Timeout:     10 * time.Second,
				Description: "输入用户名",
			},
			{
				Command:     "{password}",
				Expect:      `[>#]\s*$`,
				Response:    "",
				Timeout:     10 * time.Second,
				Description: "输入密码",
			},
		},
	}
	
	return session
}

// CreatePrivilegeSession 创建特权模式会话
func (h *HuaweiInteractor) CreatePrivilegeSession() *InteractionSession {
	return &InteractionSession{
		DeviceType: "privilege",
		Context:    make(map[string]string),
		Steps: []*InteractionStep{
			{
				Command:     "system-view",
				Expect:      `\[.*\]\s*$`,
				Response:    "",
				Timeout:     5 * time.Second,
				Description: "进入系统视图",
			},
		},
	}
}

// CreateConfigSession 创建配置会话
func (h *HuaweiInteractor) CreateConfigSession(configCommands []string) *InteractionSession {
	session := &InteractionSession{
		DeviceType: "configuration",
		Context:    make(map[string]string),
		Steps:      []*InteractionStep{},
	}
	
	// 进入系统视图
	session.Steps = append(session.Steps, &InteractionStep{
		Command:     "system-view",
		Expect:      `\[.*\]\s*$`,
		Response:    "",
		Timeout:     5 * time.Second,
		Description: "进入系统视图",
	})
	
	// 添加配置命令
	for _, cmd := range configCommands {
		session.Steps = append(session.Steps, &InteractionStep{
			Command:     cmd,
			Expect:      `\[.*\]\s*$`,
			Response:    "",
			Timeout:     10 * time.Second,
			Description: fmt.Sprintf("执行配置命令: %s", cmd),
		})
	}
	
	// 保存配置
	session.Steps = append(session.Steps, &InteractionStep{
		Command:     "save",
		Expect:      `(?i)(y/n|yes/no)`,
		Response:    "y",
		Timeout:     5 * time.Second,
		Description: "保存配置",
	})
	
	session.Steps = append(session.Steps, &InteractionStep{
		Command:     "y",
		Expect:      `\[.*\]\s*$`,
		Response:    "",
		Timeout:     30 * time.Second,
		Description: "确认保存",
	})
	
	// 退出系统视图
	session.Steps = append(session.Steps, &InteractionStep{
		Command:     "quit",
		Expect:      `[>#]\s*$`,
		Response:    "",
		Timeout:     5 * time.Second,
		Description: "退出系统视图",
	})
	
	return session
}

// HandlePrompt 处理设备提示符
func (h *HuaweiInteractor) HandlePrompt(output string) (string, bool) {
	// 华为设备常见提示符模式
	prompts := []string{
		`<[^>]+>\s*$`,           // 用户视图: <Huawei>
		`\[[^\]]+\]\s*$`,        // 系统视图: [Huawei]
		`\[[^\]]+\-[^\]]+\]\s*$`, // 接口视图: [Huawei-GigabitEthernet0/0/1]
		`[^#]+#\s*$`,            // 特权模式
	}
	
	for _, pattern := range prompts {
		if matched, _ := regexp.MatchString(pattern, output); matched {
			// 提取提示符
			re := regexp.MustCompile(pattern)
			matches := re.FindStringSubmatch(output)
			if len(matches) > 0 {
				return strings.TrimSpace(matches[0]), true
			}
		}
	}
	
	return "", false
}

// DetectError 检测错误信息
func (h *HuaweiInteractor) DetectError(output string) (bool, string) {
	errorPatterns := []struct {
		pattern string
		message string
	}{
		{`Error:\s*(.+)`, "命令执行错误"},
		{`Invalid command`, "无效命令"},
		{`Unrecognized command`, "无法识别的命令"},
		{`Incomplete command`, "命令不完整"},
		{`Too many parameters`, "参数过多"},
		{`Authentication failed`, "认证失败"},
		{`Permission denied`, "权限不足"},
		{`Connection refused`, "连接被拒绝"},
		{`Timeout`, "操作超时"},
	}
	
	for _, ep := range errorPatterns {
		if matched, _ := regexp.MatchString(ep.pattern, output); matched {
			return true, ep.message
		}
	}
	
	return false, ""
}

// ProcessInteractiveCommand 处理交互式命令
func (h *HuaweiInteractor) ProcessInteractiveCommand(ctx context.Context, command string, responses []string) ([]*InteractionStep, error) {
	var steps []*InteractionStep
	
	// 根据命令类型创建交互步骤
	switch {
	case strings.Contains(command, "save"):
		steps = append(steps, &InteractionStep{
			Command:     command,
			Expect:      `(?i)(y/n|yes/no)`,
			Response:    "y",
			Timeout:     10 * time.Second,
			Description: "保存配置确认",
		})
		
	case strings.Contains(command, "reset"):
		steps = append(steps, &InteractionStep{
			Command:     command,
			Expect:      `(?i)(y/n|yes/no)`,
			Response:    "y",
			Timeout:     10 * time.Second,
			Description: "重置确认",
		})
		
	case strings.Contains(command, "reboot") || strings.Contains(command, "restart"):
		steps = append(steps, &InteractionStep{
			Command:     command,
			Expect:      `(?i)(y/n|yes/no)`,
			Response:    "y",
			Timeout:     10 * time.Second,
			Description: "重启确认",
		})
		
	default:
		// 普通命令
		steps = append(steps, &InteractionStep{
			Command:     command,
			Expect:      `[>#\]]\s*$`,
			Response:    "",
			Timeout:     30 * time.Second,
			Description: fmt.Sprintf("执行命令: %s", command),
		})
	}
	
	// 添加用户自定义响应
	for i, response := range responses {
		if i < len(steps) {
			steps[i].Response = response
		}
	}
	
	return steps, nil
}

// GetDeviceCapabilities 获取设备能力
func (h *HuaweiInteractor) GetDeviceCapabilities(deviceModel string) map[string]interface{} {
	capabilities := map[string]interface{}{
		"supports_telnet":     true,
		"supports_ssh":        true,
		"supports_snmp":       true,
		"supports_netconf":    false,
		"max_sessions":        5,
		"config_rollback":     true,
		"batch_config":        true,
		"file_transfer":       true,
	}
	
	// 根据设备型号调整能力
	switch {
	case strings.Contains(strings.ToUpper(deviceModel), "S57"):
		capabilities["supports_netconf"] = true
		capabilities["max_sessions"] = 10
	case strings.Contains(strings.ToUpper(deviceModel), "S67"):
		capabilities["supports_netconf"] = true
		capabilities["max_sessions"] = 15
	case strings.Contains(strings.ToUpper(deviceModel), "S97"):
		capabilities["supports_netconf"] = true
		capabilities["max_sessions"] = 20
	case strings.Contains(strings.ToUpper(deviceModel), "AR"):
		capabilities["supports_netconf"] = true
		capabilities["max_sessions"] = 8
	}
	
	return capabilities
}

// ValidateSession 验证会话状态
func (h *HuaweiInteractor) ValidateSession(output string) (bool, string) {
	// 检查是否已登录
	loginPatterns := []string{
		`<[^>]+>\s*$`,    // 用户视图
		`\[[^\]]+\]\s*$`, // 系统视图
		`[^#]+#\s*$`,     // 特权模式
	}
	
	for _, pattern := range loginPatterns {
		if matched, _ := regexp.MatchString(pattern, output); matched {
			return true, "session_active"
		}
	}
	
	// 检查登录提示
	if matched, _ := regexp.MatchString(`(?i)(username|login):\s*$`, output); matched {
		return false, "login_required"
	}
	
	if matched, _ := regexp.MatchString(`(?i)password:\s*$`, output); matched {
		return false, "password_required"
	}
	
	// 检查连接状态
	if strings.Contains(output, "Connection refused") || strings.Contains(output, "Connection closed") {
		return false, "connection_failed"
	}
	
	return false, "unknown_state"
}

// GetOptimizedTimeout 获取优化的超时时间
func (h *HuaweiInteractor) GetOptimizedTimeout(command string) time.Duration {
	// 根据命令类型返回优化的超时时间
	switch {
	case strings.Contains(command, "save"):
		return 60 * time.Second
	case strings.Contains(command, "display current"):
		return 120 * time.Second
	case strings.Contains(command, "reboot") || strings.Contains(command, "restart"):
		return 300 * time.Second
	case strings.Contains(command, "copy") || strings.Contains(command, "backup"):
		return 180 * time.Second
	default:
		return 30 * time.Second
	}
}