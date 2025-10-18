package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/internal/service"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// FormattedHandler 数据格式化处理器
type FormattedHandler struct {
	formatService *service.FormatService
}

// NewFormattedHandler 创建格式化处理器
func NewFormattedHandler(formatService *service.FormatService) *FormattedHandler {
	return &FormattedHandler{formatService: formatService}
}

// BatchFormatted 批量格式化接口
// @Summary 批量格式化并存储数据
// @Description 读取设备参数、采集结果，结合 FSM 模板生成聚合格式化结果并存储至 MinIO
// @Tags formatted
// @Accept json
// @Produce json
// @Param request body service.FormatBatchRequest true "批量格式化请求"
// @Success 200 {object} service.FormatBatchResponse "批量格式化结果"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/formatted/batch [post]
func (h *FormattedHandler) BatchFormatted(c *gin.Context) {
	var req service.FormatBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid formatted batch request", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "请求参数无效: " + err.Error()})
		return
	}

	if h.formatService == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "SERVICE_NOT_READY", Message: "格式化服务未初始化"})
		return
	}

	resp, err := h.formatService.ExecuteBatch(c.Request.Context(), &req)
	if err != nil {
		logger.Error("Formatted batch execution failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "EXEC_FAILED", Message: "批量格式化执行失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// FastFormatted 单设备快速格式化接口
// @Summary 单设备快速格式化
// @Description 复用登录与采集能力，仅返回格式化 JSON，不强制存储
// @Tags formatted
// @Accept json
// @Produce json
// @Param request body service.FormatFastRequest true "快速格式化请求"
// @Success 200 {object} service.FormatFastResponse "快速格式化结果"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/formatted/fast [post]
func (h *FormattedHandler) FastFormatted(c *gin.Context) {
	var req service.FormatFastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid formatted fast request", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "请求参数无效: " + err.Error()})
		return
	}

	if h.formatService == nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "SERVICE_NOT_READY", Message: "格式化服务未初始化"})
		return
	}

	resp, err := h.formatService.ExecuteFast(c.Request.Context(), &req)
	if err != nil {
		logger.Error("Formatted fast execution failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "EXEC_FAILED", Message: "快速格式化执行失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
