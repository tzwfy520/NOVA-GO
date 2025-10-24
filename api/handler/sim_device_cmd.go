package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"gorm.io/gorm"
)

// SimDeviceCmdHandler 针对命名空间与设备的模拟命令处理器
// 路由建议：/api/v1/sim-device-cmds
// 支持：查询（按namespace、device_name、enabled）、创建、查看、更新、删除

type SimDeviceCmdHandler struct{}

func NewSimDeviceCmdHandler() *SimDeviceCmdHandler { return &SimDeviceCmdHandler{} }

// CreateSimDeviceCmd 创建模拟命令
func (h *SimDeviceCmdHandler) CreateSimDeviceCmd(c *gin.Context) {
	var req model.SimDeviceCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数错误: " + err.Error()})
		return
	}
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	req.Command = strings.TrimSpace(req.Command)
	if req.Namespace == "" || req.DeviceName == "" || req.Command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MISSING_FIELDS", "message": "namespace、device_name 与 command 不能为空"})
		return
	}
	// 默认启用
	if !req.Enabled {
		req.Enabled = true
	}

	// 并发保护：检测到 SQLite Busy 时进行短暂重试
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Create(&req).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Create sim device command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "创建失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "SUCCESS", "message": "创建成功", "data": req})
}

// ListSimDeviceCmds 列出模拟命令（按命名空间与设备筛选）
func (h *SimDeviceCmdHandler) ListSimDeviceCmds(c *gin.Context) {
	ns := strings.TrimSpace(c.Query("namespace"))
	dev := strings.TrimSpace(c.Query("device_name"))
	enabledQ := strings.TrimSpace(c.Query("enabled"))

	db := database.GetDB()
	var items []model.SimDeviceCommand
	q := db.Model(&model.SimDeviceCommand{})
	if ns != "" {
		q = q.Where("namespace = ?", ns)
	}
	if dev != "" {
		q = q.Where("device_name = ?", dev)
	}
	if enabledQ != "" {
		switch enabledQ {
		case "true":
			q = q.Where("enabled = 1")
		case "false":
			q = q.Where("enabled = 0")
		}
	}
	if err := q.Order("updated_at DESC").Find(&items).Error; err != nil {
		logger.Error("List sim device commands failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "LIST_FAILED", "message": "查询失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "查询成功", "data": items})
}

// GetSimDeviceCmd 查看单条模拟命令
func (h *SimDeviceCmdHandler) GetSimDeviceCmd(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID不合法"})
		return
	}
	db := database.GetDB()
	var item model.SimDeviceCommand
	if err := db.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "记录不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "查询成功", "data": item})
}

// UpdateSimDeviceCmd 更新模拟命令（支持禁用/启用、修改命令与回显）
func (h *SimDeviceCmdHandler) UpdateSimDeviceCmd(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID不合法"})
		return
	}
	var req model.SimDeviceCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数错误: " + err.Error()})
		return
	}

	db := database.GetDB()
	var item model.SimDeviceCommand
	if err := db.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "记录不存在"})
		return
	}
	update := map[string]interface{}{}
	if strings.TrimSpace(req.Command) != "" {
		update["command"] = strings.TrimSpace(req.Command)
	}
	if req.Output != "" {
		update["output"] = req.Output
	}
	// 允许切换启用状态
	update["enabled"] = req.Enabled
	// 可选：允许变更namespace/device_name（需要提供且非空）
	if strings.TrimSpace(req.Namespace) != "" {
		update["namespace"] = strings.TrimSpace(req.Namespace)
	}
	if strings.TrimSpace(req.DeviceName) != "" {
		update["device_name"] = strings.TrimSpace(req.DeviceName)
	}

	if err := database.WithRetry(func(d *gorm.DB) error { return d.Model(&item).Updates(update).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Update sim device command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "更新成功"})
}

// DeleteSimDeviceCmd 删除模拟命令
func (h *SimDeviceCmdHandler) DeleteSimDeviceCmd(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.Atoi(idStr)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID不合法"})
		return
	}
	// 并发保护：检测到 SQLite Busy 时进行短暂重试
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Delete(&model.SimDeviceCommand{}, id).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Delete sim device command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "删除成功"})
}
