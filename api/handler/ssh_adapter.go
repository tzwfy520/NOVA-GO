package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// SSHAdapterHandler SSH适配处理器
type SSHAdapterHandler struct{}

func NewSSHAdapterHandler() *SSHAdapterHandler { return &SSHAdapterHandler{} }

// CreatePlatformRequest 新增平台请求
type CreatePlatformRequest struct {
	SSHType string `json:"ssh_type" binding:"required"`
	Vendor  string `json:"vendor"`
	System  string `json:"system"`
	Remark  string `json:"remark"`
}

// UpdatePlatformRequest 更新平台元信息请求
type UpdatePlatformRequest struct {
	Vendor string `json:"vendor"`
	System string `json:"system"`
	Remark string `json:"remark"`
}

// UpdateParamsRequest 更新平台适配参数请求（任意JSON对象）
type UpdateParamsRequest map[string]interface{}

// ListPlatforms 列出所有平台（不含大字段处理）
func (h *SSHAdapterHandler) ListPlatforms(c *gin.Context) {
	// 保证 default 的ID为1（如存在则调整，占位冲突时顺序置换）
	ensureDefaultIDOne()

	db := database.GetDB()
	var list []model.SSHPlatform
	if err := db.Order("id asc").Find(&list).Error; err != nil {
		logger.Error("List SSH platforms failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "DB_ERROR", Message: "查询平台列表失败: " + err.Error()})
		return
	}

	// 读取 configs/auto-ssh.yaml 的 collector.device_defaults，按平台补全缺失条目
	// 数据源优先级：数据库 > auto-ssh.yaml（仅用于补全，不覆盖已存在条目）
	present := map[string]struct{}{}
	for _, p := range list { present[p.Type] = struct{}{} }
	if entries, err := loadConfigDeviceDefaultsEntries(filepath.Join("configs", "auto-ssh.yaml")); err == nil {
		for _, e := range entries {
			if _, ok := present[e.Type]; ok {
				// 已存在：若 YAML 提供厂商/系统/备注，且数据库为空，则补充更新
				if e.Vendor != "" || e.System != "" || e.Remark != "" {
					var p model.SSHPlatform
					if err2 := db.Where("ssh_type = ?", e.Type).First(&p).Error; err2 == nil {
						changed := false
						if p.Vendor == "" && e.Vendor != "" { p.Vendor = e.Vendor; changed = true }
						if p.System == "" && e.System != "" { p.System = e.System; changed = true }
						if p.Remark == "" && e.Remark != "" { p.Remark = e.Remark; changed = true }
						if changed {
							if err2 := database.WithRetry(func(d *gorm.DB) error { return d.Save(&p).Error }, 6, 100*time.Millisecond); err2 != nil {
								logger.Error("Auto update platform meta from YAML failed", "type", e.Type, "error", err)
							}
						}
					}
				}
				continue
			}
			var obj map[string]interface{}
			if err2 := yaml.Unmarshal([]byte(e.YAML), &obj); err2 != nil {
				logger.Error("Parse YAML platform entry failed", "type", e.Type, "error", err)
				continue
			}
			paramsJSON, _ := json.Marshal(obj)
			p := model.SSHPlatform{ Type: e.Type, Vendor: e.Vendor, System: e.System, Remark: "imported from auto-ssh.yaml", Params: string(paramsJSON) }
			if err2 := database.WithRetry(func(d *gorm.DB) error { return d.Create(&p).Error }, 6, 100*time.Millisecond); err2 != nil {
				logger.Error("Auto import platform from YAML failed", "type", e.Type, "error", err)
				continue
			}
			list = append(list, p)
			present[e.Type] = struct{}{}
		}
	} else {
		logger.Debug("auto-ssh.yaml not loaded for platform import", "error", err)
	}

	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "OK", Data: list})
}

// 保证 default 的ID为1；如已被占用则让占用者移至最大ID+1
func ensureDefaultIDOne() {
	db := database.GetDB()
	var def model.SSHPlatform
	if err := db.Where("ssh_type = ?", "default").First(&def).Error; err != nil {
		return
	}
	if def.ID == 1 {
		return
	}
	// 开启事务进行ID调整
	tx := db.Begin()
	defer func(){ if r := recover(); r != nil { _ = tx.Rollback() } }()
	// 如ID=1存在且不是default，则将其挪到最大ID+1
	var exist model.SSHPlatform
	if err := tx.First(&exist, 1).Error; err == nil && exist.Type != "default" {
		var maxID uint
		row := tx.Model(&model.SSHPlatform{}).Select("MAX(id)").Row()
		var mx int64
		if scanErr := row.Scan(&mx); scanErr == nil && mx >= 0 { maxID = uint(mx) }
		if maxID < 1 { maxID = 1 }
		if err := tx.Exec("UPDATE ssh_platforms SET id = ? WHERE id = ?", maxID+1, exist.ID).Error; err != nil {
			_ = tx.Rollback()
			return
		}
	}
	// 将 default 的ID设为1
	if err := tx.Exec("UPDATE ssh_platforms SET id = 1 WHERE id = ?", def.ID).Error; err != nil {
		_ = tx.Rollback()
		return
	}
	_ = tx.Commit().Error
}

// CreatePlatform 新增平台，默认填充示例参数
func (h *SSHAdapterHandler) CreatePlatform(c *gin.Context) {
	var req CreatePlatformRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.SSHType == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "参数错误或缺少ssh_type"})
		return
	}

	// 规范化类型命名：下划线连接（已存在保持原样）
	sshType := req.SSHType

	db := database.GetDB()
	var exists model.SSHPlatform
	if err := db.Where("ssh_type = ?", sshType).First(&exists).Error; err == nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "DUPLICATE", Message: "该ssh_type已存在"})
		return
	}

	params := defaultParamsFor(sshType)
	paramsJSON, _ := json.Marshal(params)

	p := model.SSHPlatform{
		Type:   sshType,
		Vendor: req.Vendor,
		System: req.System,
		Remark: req.Remark,
		Params: string(paramsJSON),
	}
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Create(&p).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Create SSH platform failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "DB_ERROR", Message: "创建平台失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, SuccessResponse{Code: "SUCCESS", Message: "创建成功", Data: p})
}

// GetPlatform 获取单个平台详情
func (h *SSHAdapterHandler) GetPlatform(c *gin.Context) {
	id := c.Param("id")
	db := database.GetDB()
	var p model.SSHPlatform
	if err := db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Code: "NOT_FOUND", Message: "平台不存在"})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "OK", Data: p})
}

// UpdatePlatform 更新平台元信息（不允许修改ssh_type）
func (h *SSHAdapterHandler) UpdatePlatform(c *gin.Context) {
	id := c.Param("id")
	var req UpdatePlatformRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "参数错误"})
		return
	}

	db := database.GetDB()
	var p model.SSHPlatform
	if err := db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Code: "NOT_FOUND", Message: "平台不存在"})
		return
	}

	p.Vendor = req.Vendor
	p.System = req.System
	p.Remark = req.Remark
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Save(&p).Error }, 6, 100*time.Millisecond); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "DB_ERROR", Message: "更新失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "更新成功", Data: p})
}

// DeletePlatform 删除平台（default 不允许删除）
func (h *SSHAdapterHandler) DeletePlatform(c *gin.Context) {
	id := c.Param("id")
	db := database.GetDB()
	var p model.SSHPlatform
	if err := db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Code: "NOT_FOUND", Message: "平台不存在"})
		return
	}
	if p.Type == "default" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "FORBIDDEN", Message: "default 类型不允许删除"})
		return
	}
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Delete(&p).Error }, 6, 100*time.Millisecond); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "DB_ERROR", Message: "删除失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "删除成功", Data: gin.H{"id": id}})
}

// GetParams 获取平台适配参数（JSON）
func (h *SSHAdapterHandler) GetParams(c *gin.Context) {
	id := c.Param("id")
	db := database.GetDB()
	var p model.SSHPlatform
	if err := db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Code: "NOT_FOUND", Message: "平台不存在"})
		return
	}
	var params map[string]interface{}
	if p.Params != "" {
		_ = json.Unmarshal([]byte(p.Params), &params)
	}
	if params == nil {
		params = defaultParamsFor(p.Type)
	}
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "OK", Data: params})
}

// UpdateParams 更新平台参数（整对象覆盖）
func (h *SSHAdapterHandler) UpdateParams(c *gin.Context) {
	id := c.Param("id")
	var body UpdateParamsRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "参数必须为JSON对象"})
		return
	}

	paramsJSON, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_JSON", Message: "参数序列化失败: " + err.Error()})
		return
	}

	db := database.GetDB()
	var p model.SSHPlatform
	if err := db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Code: "NOT_FOUND", Message: "平台不存在"})
		return
	}
	p.Params = string(paramsJSON)
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Save(&p).Error }, 6, 100*time.Millisecond); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "DB_ERROR", Message: "保存失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "保存成功", Data: gin.H{"id": p.ID}})
}

// GenerateYAML 生成 auto-ssh.yaml 文件（根据SQLite内容）
func (h *SSHAdapterHandler) GenerateYAML(c *gin.Context) {
	db := database.GetDB()
	var list []model.SSHPlatform
	if err := db.Order("ssh_type asc").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "DB_ERROR", Message: "查询失败: " + err.Error()})
		return
	}
	if len(list) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "EMPTY", Message: "尚未创建任何平台"})
		return
	}

	// 构建 device_defaults 顶层与分平台块（含注释）
	var entries []platformYAMLEntry
	for _, p := range list {
		var params map[string]interface{}
		if p.Params != "" {
			_ = json.Unmarshal([]byte(p.Params), &params)
		}
		if params == nil {
			params = defaultParamsFor(p.Type)
		}
		yamlBytes, err := yaml.Marshal(params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "YAML_ERROR", Message: "序列化失败: " + err.Error()})
			return
		}
		entries = append(entries, platformYAMLEntry{
			Type:   p.Type,
			Vendor: p.Vendor,
			System: p.System,
			Remark: p.Remark,
			YAML:   string(yamlBytes),
		})
	}

	// 兼容从配置文件引入平台默认参数（collector.device_defaults）
	cfgEntries, _ := loadConfigDeviceDefaultsEntries(filepath.Join("configs", "config.yaml"))
	if len(cfgEntries) > 0 {
		present := map[string]struct{}{}
		for _, e := range entries { present[e.Type] = struct{}{} }
		for _, ce := range cfgEntries {
			if _, ok := present[ce.Type]; !ok { entries = append(entries, ce) }
		}
	}

	// 排序保证稳定输出：default 优先，其余按名称
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type == "default" && entries[j].Type != "default" { return true }
		if entries[i].Type != "default" && entries[j].Type == "default" { return false }
		return entries[i].Type < entries[j].Type
	})

	final := composeCollectorYAML(entries)

	// 写入 configs/auto-ssh.yaml
	outPath := filepath.Join("configs", "auto-ssh.yaml")
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "IO_ERROR", Message: "创建目录失败: " + err.Error()})
		return
	}
	if err := os.WriteFile(outPath, []byte(final), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "IO_ERROR", Message: "写入文件失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "YAML生成成功", Data: gin.H{"path": outPath}})
}

// GetPlatformYAML 获取指定平台生成的YAML片段（带注释）
func (h *SSHAdapterHandler) GetPlatformYAML(c *gin.Context) {
	id := c.Param("id")
	db := database.GetDB()
	var p model.SSHPlatform
	if err := db.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Code: "NOT_FOUND", Message: "平台不存在"})
		return
	}
	var params map[string]interface{}
	if p.Params != "" {
		_ = json.Unmarshal([]byte(p.Params), &params)
	}
	if params == nil {
		params = defaultParamsFor(p.Type)
	}
	yb, err := yaml.Marshal(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "YAML_ERROR", Message: "序列化失败: " + err.Error()})
		return
	}
	entry := platformYAMLEntry{Type: p.Type, Vendor: p.Vendor, System: p.System, Remark: p.Remark, YAML: string(yb)}
	snippet := composeSinglePlatformYAML(entry)
	c.JSON(http.StatusOK, SuccessResponse{Code: "SUCCESS", Message: "OK", Data: gin.H{"yaml": snippet}})
}

// ======= YAML 拼接辅助 =======

type platformYAMLEntry struct {
	Type   string
	Vendor string
	System string
	Remark string
	YAML   string // 不含顶层键，纯对象块
}


func composeSinglePlatformYAML(e platformYAMLEntry) string {
	b := &stringsBuilder{}
	b.WriteString("device_defaults:\n")
	if e.Vendor != "" { b.WriteString(fmt.Sprintf("  # vendor: %s\n", e.Vendor)) }
	if e.System != "" { b.WriteString(fmt.Sprintf("  # system: %s\n", e.System)) }
	if e.Remark != "" { b.WriteString(fmt.Sprintf("  # remark: %s\n", e.Remark)) }
	b.WriteString(fmt.Sprintf("  %s:\n", e.Type))
	b.WriteString(indent("    ", e.YAML))
	return b.String()
}

func indent(prefix, s string) string {
	lines := []byte(s)
	out := make([]byte, 0, len(lines)+len(prefix)*10)
	// 简单逐行缩进
	prev := 0
	for i := 0; i < len(lines); i++ {
		if lines[i] == '\n' {
			out = append(out, prefix...)
			out = append(out, lines[prev:i+1]...)
			prev = i + 1
		}
	}
	if prev < len(lines) {
		out = append(out, prefix...)
		out = append(out, lines[prev:]...)
	}
	return string(out)
}

// 简单 strings.Builder 包装，避免不同Go版本的import冲突
type stringsBuilder struct { b []byte }
func (sb *stringsBuilder) WriteString(s string) { sb.b = append(sb.b, s...) }
func (sb *stringsBuilder) String() string       { return string(sb.b) }

// ======= 默认参数模板（JSON结构） =======

func defaultParamsFor(sshType string) map[string]interface{} {
	switch sshType {
	case "default":
		return map[string]interface{}{
			"output_filter": map[string]interface{}{
				"prefixes":        []string{"---- More ----", "more"},
				"contains":        []string{"--more--"},
				"case_insensitive": true,
				"trim_space":       true,
			},
			"interact": map[string]interface{}{
				"auto_interactions": []map[string]string{
					{"except_output": "do you want to save this config? yes/no", "command_auto_send": "yes"},
					{"except_output": "do you want to reload this device? yes/no", "command_auto_send": "no"},
				},
				"error_hints":     []string{"ERROR:", "invalid parameters detect"},
				"case_insensitive": true,
				"trim_space":       true,
			},
		}
	case "cisco_ios":
		return map[string]interface{}{
			"prompt_suffixes":    []string{">", "#"},
			"disable_paging_cmds": []string{"terminal length 0"},
			"config_mode_clis":   []string{"configure terminal"},
			"config_exit_cli":    "end",
			"enable_required":    true,
			"enable_cli":         "enable",
			"enable_except_output": "Password:",
			"skip_delayed_echo":  true,
			"timeout": map[string]interface{}{
				"timeout_all": 60,
				"dial_timeout": 2,
				"auth_timeout": 5,
				"interact_timeout": map[string]interface{}{
					"command_interval_ms":      120,
					"command_timeout_sec":      30,
					"quiet_after_ms":           800,
					"quiet_poll_interval_ms":   250,
					"prompt_inducer_interval_ms": 1000,
					"prompt_inducer_max_count":   12,
					"exit_pause_ms":             150,
					"enable_password_fallback_ms": 1500,
				},
			},
			"output_filter": map[string]interface{}{
				"prefixes":        []string{"---- More ----", "more"},
				"contains":        []string{"--more--"},
				"case_insensitive": true,
				"trim_space":       true,
			},
			"interact": map[string]interface{}{
				"auto_interactions": []map[string]string{
					{"except_output": "--more--", "command_auto_send": " "},
					{"except_output": "more", "command_auto_send": " "},
					{"except_output": "press any key", "command_auto_send": " "},
					{"except_output": "confirm", "command_auto_send": "y"},
					{"except_output": "[yes/no]", "command_auto_send": "yes"},
				},
				"error_hints":     []string{"invalid input detected", "incomplete command", "unknown command", "invalid autocommand", "line has invalid autocommand"},
				"case_insensitive": true,
				"trim_space":       true,
			},
		}
	case "huawei":
		return map[string]interface{}{
			"prompt_suffixes":    []string{">", "#", "]"},
			"disable_paging_cmds": []string{"screen-length disable"},
			"config_mode_clis":   []string{"system-view immediately", "system-view"},
			"config_exit_cli":    "quit",
			"enable_required":    false,
			"skip_delayed_echo":  true,
			"timeout": map[string]interface{}{
				"timeout_all": 45,
				"dial_timeout": 2,
				"auth_timeout": 5,
				"interact_timeout": map[string]interface{}{
					"command_interval_ms":      120,
					"command_timeout_sec":      30,
					"quiet_after_ms":           800,
					"quiet_poll_interval_ms":   250,
					"prompt_inducer_interval_ms": 1000,
					"prompt_inducer_max_count":   12,
					"exit_pause_ms":             150,
					"enable_password_fallback_ms": 1500,
				},
			},
			"output_filter": map[string]interface{}{
				"prefixes":        []string{"---- More ----", "more"},
				"contains":        []string{"--more--"},
				"case_insensitive": true,
				"trim_space":       true,
			},
			"interact": map[string]interface{}{
				"auto_interactions": []map[string]string{
					{"except_output": "more", "command_auto_send": " "},
					{"except_output": "press any key", "command_auto_send": " "},
					{"except_output": "confirm", "command_auto_send": "y"},
				},
				"error_hints":     []string{"error:", "unrecognized command"},
				"case_insensitive": true,
				"trim_space":       true,
			},
		}
	default:
		// 其他平台以default为基础
		return map[string]interface{}{
			"output_filter": map[string]interface{}{
				"prefixes":        []string{"---- More ----", "more"},
				"contains":        []string{"--more--"},
				"case_insensitive": true,
				"trim_space":       true,
			},
			"interact": map[string]interface{}{
				"auto_interactions": []map[string]string{},
				"error_hints":     []string{"error"},
				"case_insensitive": true,
				"trim_space":       true,
			},
		}
	}
}

// 新增：生成 collector 包裹的 device_defaults 全量YAML
func composeCollectorYAML(entries []platformYAMLEntry) string {
	b := &stringsBuilder{}
	b.WriteString("collector:\n")
	b.WriteString("  device_defaults:\n")
	for _, e := range entries {
		if e.Vendor != "" { b.WriteString(fmt.Sprintf("    # vendor: %s\n", e.Vendor)) }
		if e.System != "" { b.WriteString(fmt.Sprintf("    # system: %s\n", e.System)) }
		if e.Remark != "" { b.WriteString(fmt.Sprintf("    # remark: %s\n", e.Remark)) }
		b.WriteString(fmt.Sprintf("    %s:\n", e.Type))
		b.WriteString(indent("      ", e.YAML))
	}
	return b.String()
}

// 新增：从配置文件解析 collector.device_defaults 作为平台条目（保留注释中的 vendor/system/remark）
func loadConfigDeviceDefaultsEntries(path string) ([]platformYAMLEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil { return nil, err }

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil { return nil, err }
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("invalid yaml root in %s", path)
	}
	root := doc.Content[0]

	// 查找 collector.device_defaults 或顶层 device_defaults
	dd := findMapValue(root, "collector")
	if dd != nil { dd = findMapValue(dd, "device_defaults") }
	if dd == nil { dd = findMapValue(root, "device_defaults") }
	if dd == nil || dd.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("device_defaults not found in %s", path)
	}

	entries := make([]platformYAMLEntry, 0, len(dd.Content)/2)
	for i := 0; i < len(dd.Content)-1; i += 2 {
		keyNode := dd.Content[i]
		valNode := dd.Content[i+1]
		sshType := keyNode.Value

		// 提取注释中的 vendor/system/remark（注释位于块前面）
		vendor, system, remark := "", "", ""
		if hc := strings.TrimSpace(valNode.HeadComment); hc != "" {
			for _, line := range strings.Split(hc, "\n") {
				l := strings.TrimSpace(line)
				l = strings.TrimPrefix(l, "#")
				l = strings.TrimSpace(l)
				low := strings.ToLower(l)
				if strings.HasPrefix(low, "vendor:") {
					vendor = strings.TrimSpace(l[len("vendor:"):])
				} else if strings.HasPrefix(low, "system:") {
					system = strings.TrimSpace(l[len("system:"):])
				} else if strings.HasPrefix(low, "remark:") {
					remark = strings.TrimSpace(l[len("remark:"):])
				}
			}
		}

		// 序列化该平台的 YAML 片段
		yb, err := yaml.Marshal(valNode)
		if err != nil { return nil, err }
		entries = append(entries, platformYAMLEntry{ Type: sshType, Vendor: vendor, System: system, Remark: remark, YAML: string(yb) })
	}

	// 稳定排序
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type == "default" && entries[j].Type != "default" { return true }
		if entries[i].Type != "default" && entries[j].Type == "default" { return false }
		return entries[i].Type < entries[j].Type
	})
	return entries, nil
}

// 辅助：在映射节点中按键查找子节点
func findMapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode { return nil }
	for i := 0; i < len(m.Content)-1; i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		if k.Value == key { return v }
	}
	return nil
}