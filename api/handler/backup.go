package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/internal/service"
)

// BackupHandler 备份接口处理器
type BackupHandler struct {
	svc *service.BackupService
}

func NewBackupHandler(svc *service.BackupService) *BackupHandler { return &BackupHandler{svc: svc} }

// BatchBackup 批量备份接口
func (h *BackupHandler) BatchBackup(c *gin.Context) {
	var req service.BackupBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_REQUEST", "message": err.Error()})
		return
	}
	if req.TaskID == "" || len(req.Devices) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_PARAMS", "message": "task_id and devices are required"})
		return
	}

	resp, err := h.svc.ExecuteBatch(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
