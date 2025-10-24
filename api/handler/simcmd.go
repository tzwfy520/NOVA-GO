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

// SimCmdHandler 模拟命令处理器
type SimCmdHandler struct{}

func NewSimCmdHandler() *SimCmdHandler { return &SimCmdHandler{} }

// CreateSimCmd 创建模拟命令
func (h *SimCmdHandler) CreateSimCmd(c *gin.Context) {
	var req model.SimCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数错误: " + err.Error()})
		return
	}
	req.Platform = strings.TrimSpace(req.Platform)
	req.Command = strings.TrimSpace(req.Command)
	if req.Platform == "" || req.Command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MISSING_FIELDS", "message": "platform 与 command 不能为空"})
		return
	}

	if err := database.WithRetry(func(d *gorm.DB) error { return d.Create(&req).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Create sim command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "创建失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": "SUCCESS", "message": "创建成功", "data": req})
}

// ListSimCmds 列出模拟命令（可按平台筛选）
func (h *SimCmdHandler) ListSimCmds(c *gin.Context) {
	platform := strings.TrimSpace(c.Query("platform"))
	db := database.GetDB()
	var items []model.SimCommand
	q := db.Model(&model.SimCommand{})
	if platform != "" {
		q = q.Where("platform = ?", platform)
	}
	if err := q.Order("updated_at DESC").Find(&items).Error; err != nil {
		logger.Error("List sim commands failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "LIST_FAILED", "message": "查询失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "查询成功", "data": items})
}

// UpdateSimCmd 更新模拟命令
func (h *SimCmdHandler) UpdateSimCmd(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID格式错误"})
		return
	}
	var req model.SimCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "参数错误: " + err.Error()})
		return
	}
	db := database.GetDB()
	var existing model.SimCommand
	if err := db.First(&existing, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "记录不存在"})
		return
	}
	// 仅更新允许的字段
	existing.Platform = strings.TrimSpace(req.Platform)
	existing.Command = strings.TrimSpace(req.Command)
	existing.Output = req.Output
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Save(&existing).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Update sim command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": "更新失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "更新成功", "data": existing})
}

// DeleteSimCmd 删除模拟命令
func (h *SimCmdHandler) DeleteSimCmd(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "ID格式错误"})
		return
	}
	if err := database.WithRetry(func(d *gorm.DB) error { return d.Delete(&model.SimCommand{}, uint(id)).Error }, 6, 100*time.Millisecond); err != nil {
		logger.Error("Delete sim command failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": "删除失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "删除成功", "data": gin.H{"id": id}})
}