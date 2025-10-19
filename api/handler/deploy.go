package handler

import (
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/sshcollectorpro/sshcollectorpro/internal/config"
    "github.com/sshcollectorpro/sshcollectorpro/internal/service"
)

type DeployHandler struct {
    svc *service.DeployService
}

func NewDeployHandler(svc *service.DeployService) *DeployHandler {
    return &DeployHandler{svc: svc}
}

// FastDeploy 处理 api/v1/deploy/fast
func (h *DeployHandler) FastDeploy(c *gin.Context) {
    var req service.DeployFastRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
        return
    }

    // 默认 task_type 为 exec
    if strings.TrimSpace(req.TaskType) == "" {
        req.TaskType = "exec"
    }

    // 默认超时时间：优先使用全局 ssh.timeout.timeout_all；否则回退 15s
	if req.TaskTimeout <= 0 {
		if cfg := config.Get(); cfg != nil && cfg.SSH.Timeout > 0 {
			req.TaskTimeout = int(cfg.SSH.Timeout.Seconds())
		} else {
			req.TaskTimeout = 15
		}
	}

    resp, err := h.svc.ExecuteFast(c.Request.Context(), &req)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"code": "DEPLOY_FAILED", "message": err.Error()})
        return
    }
    c.JSON(http.StatusOK, resp)
}