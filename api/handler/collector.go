package handler

import (
    "fmt"
    "net/http"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/sshcollectorpro/sshcollectorpro/internal/service"
    "github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// CollectorHandler 采集器处理器
type CollectorHandler struct {
	collectorService *service.CollectorService
}

// NewCollectorHandler 创建采集器处理器
func NewCollectorHandler(collectorService *service.CollectorService) *CollectorHandler {
	return &CollectorHandler{
		collectorService: collectorService,
	}
}

// ExecuteTask 执行采集任务
// @Summary 执行设备采集任务
// @Description 通过SSH连接设备并执行指定命令
// @Tags collector
// @Accept json
// @Produce json
// @Param request body service.CollectRequest true "采集请求"
// @Success 200 {object} service.CollectResponse "采集成功"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/execute [post]
func (h *CollectorHandler) ExecuteTask(c *gin.Context) {
    var request service.CollectRequest
    if err := c.ShouldBindJSON(&request); err != nil {
		logger.Error("Invalid request parameters", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "INVALID_PARAMS",
			Message: "请求参数无效: " + err.Error(),
		})
		return
	}

    // 参数验证
    if err := h.validateCollectRequest(&request); err != nil {
		logger.Error("Request validation failed", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "VALIDATION_FAILED",
			Message: err.Error(),
		})
		return
	}

    // 创建上下文（超时在服务层根据插件默认或传入参数处理）
    ctx := c.Request.Context()

	// 执行采集任务
    response, err := h.collectorService.ExecuteTask(ctx, &request)
	if err != nil {
		logger.Error("Failed to execute task", "task_id", request.TaskID, "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Code:    "EXECUTION_FAILED",
			Message: "任务执行失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetTaskStatus 获取任务状态
// @Summary 获取任务执行状态
// @Description 根据任务ID获取任务的执行状态和进度
// @Tags collector
// @Accept json
// @Produce json
// @Param task_id path string true "任务ID"
// @Success 200 {object} service.TaskContext "任务状态"
// @Failure 404 {object} ErrorResponse "任务不存在"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/task/{task_id}/status [get]
func (h *CollectorHandler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "MISSING_TASK_ID",
			Message: "任务ID不能为空",
		})
		return
	}

	taskContext, err := h.collectorService.GetTaskStatus(taskID)
	if err != nil {
		logger.Error("Failed to get task status", "task_id", taskID, "error", err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Code:    "TASK_NOT_FOUND",
			Message: "任务不存在: " + taskID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task_id":    taskID,
		"status":     taskContext.Status,
		"start_time": taskContext.StartTime,
		"duration":   time.Since(taskContext.StartTime),
	})
}

// CancelTask 取消任务
// @Summary 取消正在执行的任务
// @Description 根据任务ID取消正在执行的任务
// @Tags collector
// @Accept json
// @Produce json
// @Param task_id path string true "任务ID"
// @Success 200 {object} SuccessResponse "取消成功"
// @Failure 404 {object} ErrorResponse "任务不存在"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/task/{task_id}/cancel [post]
func (h *CollectorHandler) CancelTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "MISSING_TASK_ID",
			Message: "任务ID不能为空",
		})
		return
	}

	err := h.collectorService.CancelTask(taskID)
	if err != nil {
		logger.Error("Failed to cancel task", "task_id", taskID, "error", err)
		c.JSON(http.StatusNotFound, ErrorResponse{
			Code:    "TASK_NOT_FOUND",
			Message: "任务不存在: " + taskID,
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Code:    "SUCCESS",
		Message: "任务已取消",
		Data:    gin.H{"task_id": taskID},
	})
}

// GetStats 获取采集器统计信息
// @Summary 获取采集器统计信息
// @Description 获取采集器的运行状态和统计信息
// @Tags collector
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "统计信息"
// @Router /api/v1/collector/stats [get]
func (h *CollectorHandler) GetStats(c *gin.Context) {
	stats := h.collectorService.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "获取统计信息成功",
		"data":    stats,
	})
}

// Health 健康检查
// @Summary 健康检查
// @Description 检查采集器服务的健康状态
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} SuccessResponse "服务正常"
// @Failure 503 {object} ErrorResponse "服务异常"
// @Router /api/v1/health [get]
func (h *CollectorHandler) Health(c *gin.Context) {
	stats := h.collectorService.GetStats()
	
	// 检查服务是否正在运行
	if running, ok := stats["running"].(bool); !ok || !running {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Code:    "SERVICE_UNAVAILABLE",
			Message: "采集器服务未运行",
		})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{
		Code:    "SUCCESS",
		Message: "服务正常",
		Data:    stats,
	})
}

// BatchExecute 批量执行任务
// @Summary 批量执行采集任务
// @Description 批量提交多个设备的采集任务
// @Tags collector
// @Accept json
// @Produce json
// @Param requests body []service.CollectRequest true "批量采集请求"
// @Success 200 {object} []service.CollectResponse "批量采集结果"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/batch [post]
func (h *CollectorHandler) BatchExecute(c *gin.Context) {
    var requests []service.CollectRequest
	if err := c.ShouldBindJSON(&requests); err != nil {
		logger.Error("Invalid batch request parameters", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "INVALID_PARAMS",
			Message: "批量请求参数无效: " + err.Error(),
		})
		return
	}

	if len(requests) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "EMPTY_REQUESTS",
			Message: "请求列表不能为空",
		})
		return
	}

	if len(requests) > 100 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "TOO_MANY_REQUESTS",
			Message: "批量请求数量不能超过100个",
		})
		return
	}

	responses := make([]*service.CollectResponse, 0, len(requests))
	
	// 并发执行任务
	for i, request := range requests {
		// 参数验证
		if err := h.validateCollectRequest(&request); err != nil {
			responses = append(responses, &service.CollectResponse{
				TaskID:    request.TaskID,
				Success:   false,
				Error:     "参数验证失败: " + err.Error(),
				Timestamp: time.Now(),
			})
			continue
		}

        // 同步执行；超时在服务层根据插件默认或传入参数处理
        ctx := c.Request.Context()
        response, err := h.collectorService.ExecuteTask(ctx, &request)
        if err != nil {
            response = &service.CollectResponse{
                TaskID:    request.TaskID,
                Success:   false,
                Error:     err.Error(),
                Timestamp: time.Now(),
            }
        }
        
        responses = append(responses, response)

        logger.Info("Batch task completed", "index", i+1, "task_id", request.TaskID, "success", response.Success)
    }

	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "批量任务执行完成",
		"data":    responses,
		"total":   len(responses),
	})
}

// validateCollectRequest 验证采集请求参数
func (h *CollectorHandler) validateCollectRequest(request *service.CollectRequest) error {
    if strings.TrimSpace(request.TaskID) == "" {
        return fmt.Errorf("任务ID不能为空")
    }
    if strings.TrimSpace(request.DeviceIP) == "" {
        return fmt.Errorf("设备IP不能为空")
    }
    if strings.TrimSpace(request.UserName) == "" {
        return fmt.Errorf("用户名不能为空")
    }
    // 密码可为空？通常必须；若未来支持密钥则可为空。此处仍要求密码
    if strings.TrimSpace(request.Password) == "" {
        return fmt.Errorf("密码不能为空")
    }
    // collect_protocol 校验
    if p := strings.TrimSpace(strings.ToLower(request.CollectProtocol)); p != "" && p != "ssh" {
        return fmt.Errorf("不支持的采集协议: %s", request.CollectProtocol)
    }
    // system 模式需要平台
    origin := strings.TrimSpace(strings.ToLower(request.CollectOrigin))
    if origin == "system" {
        if strings.TrimSpace(request.DevicePlatform) == "" {
            return fmt.Errorf("system模式需要指定设备平台(device_platform)")
        }
    }
    // 端口（可选）范围校验
    if request.Port != 0 && (request.Port < 1 || request.Port > 65535) {
        return fmt.Errorf("端口号必须在1-65535之间")
    }
    // timeout 上限
    if request.Timeout != nil && *request.Timeout > 300 {
        return fmt.Errorf("超时时间不能超过300秒")
    }
    // retry 非负
    if request.RetryFlag != nil && *request.RetryFlag < 0 {
        return fmt.Errorf("重试次数不能为负数")
    }
    return nil
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}