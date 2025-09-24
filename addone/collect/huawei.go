package collect

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// HuaweiPlugin 华为设备采集插件
type HuaweiPlugin struct {
	Name        string
	Version     string
	Description string
	Vendor      string
}

// HuaweiDeviceInfo 华为设备信息
type HuaweiDeviceInfo struct {
	Model       string `json:"model"`
	Version     string `json:"version"`
	SerialNo    string `json:"serial_no"`
	Uptime      string `json:"uptime"`
	CPUUsage    string `json:"cpu_usage"`
	MemoryUsage string `json:"memory_usage"`
}

// HuaweiConfigSection 华为配置段
type HuaweiConfigSection struct {
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Content string            `json:"content"`
	Lines   []string          `json:"lines"`
	Params  map[string]string `json:"params"`
}

// NewHuaweiPlugin 创建华为插件实例
func NewHuaweiPlugin() *HuaweiPlugin {
	return &HuaweiPlugin{
		Name:        "huawei-collector",
		Version:     "1.0.0",
		Description: "华为设备专用采集插件，支持配置解析和设备信息提取",
		Vendor:      "Huawei",
	}
}

// GetInfo 获取插件信息
func (h *HuaweiPlugin) GetInfo() map[string]interface{} {
	return map[string]interface{}{
		"name":        h.Name,
		"version":     h.Version,
		"description": h.Description,
		"vendor":      h.Vendor,
		"supported_commands": []string{
			"display current-configuration",
			"display current",
			"display version",
			"display device",
			"display cpu-usage",
			"display memory-usage",
			"display interface brief",
			"display ip routing-table",
			"display vlan",
		},
	}
}

// ParseDeviceInfo 解析设备信息
func (h *HuaweiPlugin) ParseDeviceInfo(output string) (*HuaweiDeviceInfo, error) {
	info := &HuaweiDeviceInfo{}
	
	// 解析设备型号
	if match := regexp.MustCompile(`(?i)product\s+name\s*:\s*(.+)`).FindStringSubmatch(output); len(match) > 1 {
		info.Model = strings.TrimSpace(match[1])
	}
	
	// 解析版本信息
	if match := regexp.MustCompile(`(?i)version\s*:\s*(.+)`).FindStringSubmatch(output); len(match) > 1 {
		info.Version = strings.TrimSpace(match[1])
	}
	
	// 解析序列号
	if match := regexp.MustCompile(`(?i)serial\s+number\s*:\s*(.+)`).FindStringSubmatch(output); len(match) > 1 {
		info.SerialNo = strings.TrimSpace(match[1])
	}
	
	// 解析运行时间
	if match := regexp.MustCompile(`(?i)uptime\s*:\s*(.+)`).FindStringSubmatch(output); len(match) > 1 {
		info.Uptime = strings.TrimSpace(match[1])
	}
	
	return info, nil
}

// ParseConfiguration 解析华为配置
func (h *HuaweiPlugin) ParseConfiguration(output string) ([]*HuaweiConfigSection, error) {
	var sections []*HuaweiConfigSection
	lines := strings.Split(output, "\n")
	
	var currentSection *HuaweiConfigSection
	var sectionContent strings.Builder
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		// 检测配置段开始
		if h.isConfigSectionStart(line) {
			// 保存前一个段
			if currentSection != nil {
				currentSection.Content = sectionContent.String()
				currentSection.Lines = strings.Split(currentSection.Content, "\n")
				sections = append(sections, currentSection)
			}
			
			// 开始新段
			currentSection = &HuaweiConfigSection{
				Type:   h.getConfigSectionType(line),
				Name:   h.getConfigSectionName(line),
				Params: make(map[string]string),
			}
			sectionContent.Reset()
		}
		
		if currentSection != nil {
			sectionContent.WriteString(line + "\n")
			
			// 解析参数
			if param, value := h.parseConfigParameter(line); param != "" {
				currentSection.Params[param] = value
			}
		}
	}
	
	// 保存最后一个段
	if currentSection != nil {
		currentSection.Content = sectionContent.String()
		currentSection.Lines = strings.Split(currentSection.Content, "\n")
		sections = append(sections, currentSection)
	}
	
	return sections, nil
}

// isConfigSectionStart 判断是否为配置段开始
func (h *HuaweiPlugin) isConfigSectionStart(line string) bool {
	sectionPatterns := []string{
		`^interface\s+`,
		`^vlan\s+`,
		`^router\s+`,
		`^ospf\s+`,
		`^bgp\s+`,
		`^acl\s+`,
		`^user-interface\s+`,
		`^aaa\s*$`,
		`^snmp-agent\s*$`,
		`^ntp-service\s*$`,
	}
	
	for _, pattern := range sectionPatterns {
		if matched, _ := regexp.MatchString(pattern, line); matched {
			return true
		}
	}
	
	return false
}

// getConfigSectionType 获取配置段类型
func (h *HuaweiPlugin) getConfigSectionType(line string) string {
	if strings.HasPrefix(line, "interface") {
		return "interface"
	} else if strings.HasPrefix(line, "vlan") {
		return "vlan"
	} else if strings.HasPrefix(line, "router") {
		return "routing"
	} else if strings.HasPrefix(line, "ospf") || strings.HasPrefix(line, "bgp") {
		return "routing-protocol"
	} else if strings.HasPrefix(line, "acl") {
		return "access-control"
	} else if strings.HasPrefix(line, "user-interface") {
		return "user-interface"
	} else if strings.Contains(line, "aaa") {
		return "authentication"
	} else if strings.Contains(line, "snmp") {
		return "management"
	} else if strings.Contains(line, "ntp") {
		return "system"
	}
	
	return "general"
}

// getConfigSectionName 获取配置段名称
func (h *HuaweiPlugin) getConfigSectionName(line string) string {
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		return strings.Join(parts[1:], " ")
	}
	return parts[0]
}

// parseConfigParameter 解析配置参数
func (h *HuaweiPlugin) parseConfigParameter(line string) (string, string) {
	// 匹配 key value 格式
	if match := regexp.MustCompile(`^\s*(\S+)\s+(.+)$`).FindStringSubmatch(line); len(match) > 2 {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
	}
	
	// 匹配单独的配置项
	if match := regexp.MustCompile(`^\s*(\S+)\s*$`).FindStringSubmatch(line); len(match) > 1 {
		return strings.TrimSpace(match[1]), "enabled"
	}
	
	return "", ""
}

// GetOptimizedCommands 获取华为设备优化的命令集
func (h *HuaweiPlugin) GetOptimizedCommands(deviceType string) []string {
	baseCommands := []string{
		"display current-configuration",
		"display version",
		"display device",
	}
	
	// 根据设备类型添加特定命令
	switch strings.ToLower(deviceType) {
	case "switch", "s5700", "s6700", "s9700":
		return append(baseCommands, []string{
			"display interface brief",
			"display vlan",
			"display mac-address",
			"display stp brief",
		}...)
	case "router", "ar", "ne":
		return append(baseCommands, []string{
			"display ip routing-table",
			"display interface brief",
			"display ospf peer brief",
			"display bgp peer",
		}...)
	case "firewall", "usg":
		return append(baseCommands, []string{
			"display security-policy",
			"display interface brief",
			"display session table",
		}...)
	default:
		return baseCommands
	}
}

// ValidateOutput 验证命令输出
func (h *HuaweiPlugin) ValidateOutput(command, output string) error {
	// 检查常见错误
	errorPatterns := []string{
		`Error:`,
		`Invalid command`,
		`Unrecognized command`,
		`Incomplete command`,
		`Too many parameters`,
	}
	
	for _, pattern := range errorPatterns {
		if strings.Contains(output, pattern) {
			return fmt.Errorf("command execution failed: %s", pattern)
		}
	}
	
	// 检查输出是否为空
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("empty output for command: %s", command)
	}
	
	return nil
}

// FormatOutput 格式化输出
func (h *HuaweiPlugin) FormatOutput(command, output string) map[string]interface{} {
	result := map[string]interface{}{
		"command":   command,
		"raw_output": output,
		"timestamp": time.Now().Format(time.RFC3339),
		"plugin":    h.Name,
		"version":   h.Version,
	}
	
	// 根据命令类型进行特殊处理
	switch {
	case strings.Contains(command, "display current"):
		if sections, err := h.ParseConfiguration(output); err == nil {
			result["parsed_sections"] = sections
			result["section_count"] = len(sections)
		}
	case strings.Contains(command, "display version") || strings.Contains(command, "display device"):
		if info, err := h.ParseDeviceInfo(output); err == nil {
			result["device_info"] = info
		}
	case strings.Contains(command, "display interface"):
		result["interface_info"] = h.parseInterfaceInfo(output)
	}
	
	return result
}

// parseInterfaceInfo 解析接口信息
func (h *HuaweiPlugin) parseInterfaceInfo(output string) []map[string]string {
	var interfaces []map[string]string
	lines := strings.Split(output, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Interface") {
			continue
		}
		
		// 解析接口行格式: Interface Physical Protocol InUti OutUti inErrors outErrors
		fields := strings.Fields(line)
		if len(fields) >= 4 {
			iface := map[string]string{
				"name":     fields[0],
				"physical": fields[1],
				"protocol": fields[2],
			}
			
			if len(fields) >= 6 {
				iface["in_utilization"] = fields[3]
				iface["out_utilization"] = fields[4]
			}
			
			interfaces = append(interfaces, iface)
		}
	}
	
	return interfaces
}