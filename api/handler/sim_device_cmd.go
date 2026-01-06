package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// SimDeviceCmdHandler 针对命名空间与设备的模拟命令处理器
// 路由建议：/api/v1/sim-device-cmds
// 支持：查询（按namespace、device_name、enabled）、创建、查看、更新、删除

type SimDeviceCmdHandler struct{}

func NewSimDeviceCmdHandler() *SimDeviceCmdHandler { return &SimDeviceCmdHandler{} }

// 辅助：规范化命令文本（压缩空白并小写化，仅用于匹配，不更改存储原文）
func normalizeCommand(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

// 辅助：按位置进行前缀匹配（每个输入词必须匹配候选命令对应位置词的前缀）
func prefixMatchByWords(input string, candidate string) bool {
	in := strings.Fields(normalizeCommand(input))
	cand := strings.Fields(normalizeCommand(candidate))
	if len(in) == 0 {
		return false
	}
	if len(cand) < len(in) {
		return false
	}
	for i := 0; i < len(in); i++ {
		if !strings.HasPrefix(cand[i], in[i]) {
			return false
		}
	}
	return true
}

// CreateSimDeviceCmd 创建模拟命令（同设备同命令唯一：如已存在则更新最新回显）
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

	db := database.GetDB()
	// 先查是否已存在同设备同命令（忽略大小写）
	var existing model.SimDeviceCommand
	if err := db.Where("namespace = ? AND device_name = ? AND LOWER(command) = LOWER(?)", req.Namespace, req.DeviceName, req.Command).First(&existing).Error; err == nil {
		// 已存在：更新其回显与启用状态，保留最新
		update := map[string]interface{}{"output": req.Output, "enabled": req.Enabled}
		if err := database.WithRetry(func(d *gorm.DB) error { return d.Model(&existing).Updates(update).Error }, 6, 100*time.Millisecond); err != nil {
			logger.Error("Upsert sim device command failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "UPSERT_FAILED", "message": "更新失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "已更新现有记录", "data": existing})
		return
	}
	// 不存在：创建新记录
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

// UpdateSimDeviceCmd 更新模拟命令（唯一约束：合并到同设备同命令记录并保留最新回显）
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

	newNS := strings.TrimSpace(req.Namespace)
	if newNS == "" {
		newNS = item.Namespace
	}
	newDev := strings.TrimSpace(req.DeviceName)
	if newDev == "" {
		newDev = item.DeviceName
	}
	newCmd := strings.TrimSpace(req.Command)
	if newCmd == "" {
		newCmd = item.Command
	}

	// 查找是否存在另一条同设备同命令记录（忽略大小写）
	var other model.SimDeviceCommand
	if err := db.Where("namespace = ? AND device_name = ? AND LOWER(command) = LOWER(?)", newNS, newDev, newCmd).First(&other).Error; err == nil && other.ID != item.ID {
		// 合并：更新另一个记录的输出与启用状态，删除当前记录
		upd := map[string]interface{}{}
		if strings.TrimSpace(req.Output) != "" {
			upd["output"] = req.Output
		} else {
			upd["output"] = item.Output
		}
		upd["enabled"] = req.Enabled
		if err := database.WithRetry(func(d *gorm.DB) error { return d.Model(&other).Updates(upd).Error }, 6, 100*time.Millisecond); err != nil {
			logger.Error("Merge sim device command failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "MERGE_FAILED", "message": "合并失败: " + err.Error()})
			return
		}
		_ = database.WithRetry(func(d *gorm.DB) error { return d.Delete(&item).Error }, 6, 100*time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "已合并到唯一记录", "data": other})
		return
	}

	// 正常更新当前记录
	update := map[string]interface{}{}
	if newNS != item.Namespace {
		update["namespace"] = newNS
	}
	if newDev != item.DeviceName {
		update["device_name"] = newDev
	}
	if newCmd != item.Command {
		update["command"] = newCmd
	}
	if strings.TrimSpace(req.Output) != "" {
		update["output"] = req.Output
	}
	update["enabled"] = req.Enabled
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Model(&item).Updates(update).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Update sim device command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "更新成功", "data": item})
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

// 新增：按命名空间与设备进行命令模糊匹配，返回模拟回显或候选列表
func (h *SimDeviceCmdHandler) MatchSimDeviceCmd(c *gin.Context) {
	var req struct {
		Namespace   string `json:"namespace"`
		DeviceName  string `json:"device_name"`
		Command     string `json:"command"`
		EnabledOnly bool   `json:"enabled_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数错误: " + err.Error()})
		return
	}
	ns := strings.TrimSpace(req.Namespace)
	dev := strings.TrimSpace(req.DeviceName)
	input := strings.TrimSpace(req.Command)
	if ns == "" || dev == "" || input == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MISSING_FIELDS", "message": "namespace、device_name 与 command 不能为空"})
		return
	}

	db := database.GetDB()
	var items []model.SimDeviceCommand
	q := db.Where("namespace = ? AND device_name = ?", ns, dev)
	if req.EnabledOnly {
		q = q.Where("enabled = 1")
	}
	if err := q.Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "查询失败: " + err.Error()})
		return
	}

	inNorm := normalizeCommand(input)
	var exact *model.SimDeviceCommand
	var candidates []model.SimDeviceCommand
	for _, it := range items {
		cmdNorm := normalizeCommand(it.Command)
		if inNorm == cmdNorm {
			exact = &it
			break
		}
		if prefixMatchByWords(input, it.Command) {
			candidates = append(candidates, it)
		}
	}

	if exact != nil {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "exact", "data": gin.H{"match_type": "exact", "output": exact.Output}})
		return
	}
	if len(candidates) == 1 {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "partial_single", "data": gin.H{"match_type": "partial_single", "output": candidates[0].Output}})
		return
	}
	if len(candidates) > 1 {
		var lines []string
		lines = append(lines, "which command do you mean?")
		for _, it := range candidates {
			lines = append(lines, " -- "+strings.TrimSpace(it.Command))
		}
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "partial_multi", "data": gin.H{"match_type": "partial_multi", "output": strings.Join(lines, "\n")}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "none", "data": gin.H{"match_type": "none", "output": "unspport command"}})
}
