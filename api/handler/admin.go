package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// AdminHandler 管理相关处理器
type AdminHandler struct{}

func NewAdminHandler() *AdminHandler { return &AdminHandler{} }

// DeviceDefaultsUpdate 可更新的设备平台默认参数（部分字段）
type DeviceDefaultsUpdate struct {
	PromptSuffixes    []string `json:"prompt_suffixes"`
	DisablePagingCmds []string `json:"disable_paging_cmds"`
	EnableRequired    *bool    `json:"enable_required"`
	SkipDelayedEcho   *bool    `json:"skip_delayed_echo"`
	ConfigModeCLIs    []string `json:"config_mode_clis"`
	ConfigExitCLI     string   `json:"config_exit_cli"`
}

// GetDeviceDefaults 获取设备平台默认适配参数
func (h *AdminHandler) GetDeviceDefaults(c *gin.Context) {
	cfg := config.Get()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CONFIG_MISSING", "message": "配置未初始化"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "获取设备平台默认参数成功",
		"data":    cfg.Collector.DeviceDefaults,
	})
}

// UpdateDeviceDefaults 更新指定平台的默认适配参数（内存生效，暂不持久化）
func (h *AdminHandler) UpdateDeviceDefaults(c *gin.Context) {
	platform := c.Param("platform")
	if platform == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PLATFORM", "message": "平台名不能为空"})
		return
	}

	var req DeviceDefaultsUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid device defaults update", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数解析失败: " + err.Error()})
		return
	}

	cfg := config.Get()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CONFIG_MISSING", "message": "配置未初始化"})
		return
	}

	// 读取或初始化该平台配置
	dd := cfg.Collector.DeviceDefaults[platform]
	// 如果该平台不存在，创建默认空配置
	// 注意：新建后被热更新覆盖的可能性仍在，这里强调运行时生效
	// 用户可后续选择持久化到 configs/config.yaml

	// 应用更新
	if req.PromptSuffixes != nil {
		dd.PromptSuffixes = req.PromptSuffixes
	}
	if req.DisablePagingCmds != nil {
		dd.DisablePagingCmds = req.DisablePagingCmds
	}
	if req.EnableRequired != nil {
		dd.EnableRequired = *req.EnableRequired
	}
	if req.SkipDelayedEcho != nil {
		dd.SkipDelayedEcho = *req.SkipDelayedEcho
	}
	if req.ConfigModeCLIs != nil {
		dd.ConfigModeCLIs = req.ConfigModeCLIs
	}
	if req.ConfigExitCLI != "" {
		dd.ConfigExitCLI = req.ConfigExitCLI
	}

	cfg.Collector.DeviceDefaults[platform] = dd

	logger.Info("Device defaults updated", "platform", platform)
	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "更新成功（仅运行时生效）",
		"data":    cfg.Collector.DeviceDefaults[platform],
	})
}