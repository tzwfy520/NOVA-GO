package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/internal/service"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"golang.org/x/sync/errgroup"
	// 新增导入
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"gorm.io/gorm"
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
// 单设备接口已移除；请使用 /api/v1/collector/batch/custom 或 /api/v1/collector/batch/system

// FastCollect 快速采集（不记录任务，仅返回日志）
// @Summary 快速采集设备信息（不进行任务记录）
// @Description 通过SSH快速连接设备并执行命令，仅返回日志与输出，不写任务记录
// @Tags collector
// @Accept json
// @Produce json
// @Param request body FastCollectRequest true "快速采集请求"
// @Success 200 {object} map[string]interface{} "快速采集结果"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/fast [post]
type FastCollectRequest struct {
	DeviceIP        string   `json:"device_ip"`
	DevicePort      int      `json:"device_port,omitempty"`
	DeviceName      string   `json:"device_name,omitempty"`
	DevicePlatform  string   `json:"device_platform,omitempty"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	RetryFlag       *int     `json:"retry_flag,omitempty"`
	Timeout         *int     `json:"timeout,omitempty"`       // 兼容示例中的 timeout
	TaskTimeout     *int     `json:"task_timeout,omitempty"`  // 同义字段
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list"`
	DeviceTimeout   *int     `json:"device_timeout,omitempty"`
}

func (h *CollectorHandler) FastCollect(c *gin.Context) {
	var req FastCollectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "请求参数无效: " + err.Error()})
		return
	}

	// 组装服务层请求，兼容 timeout / task_timeout
	var effTimeout *int
	if req.Timeout != nil && *req.Timeout > 0 {
		effTimeout = req.Timeout
	} else if req.TaskTimeout != nil && *req.TaskTimeout > 0 {
		effTimeout = req.TaskTimeout
	}
	// 默认协议为 ssh
	proto := strings.TrimSpace(strings.ToLower(req.CollectProtocol))
	if proto == "" { proto = "ssh" }

	r := service.CollectRequest{
		TaskID:          fmt.Sprintf("fast-%d", time.Now().UnixNano()),
		CollectOrigin:   "fast",
		DeviceIP:        req.DeviceIP,
		Port:            req.DevicePort,
		DeviceName:      req.DeviceName,
		DevicePlatform:  req.DevicePlatform,
		CollectProtocol: proto,
		UserName:        req.UserName,
		Password:        req.Password,
		EnablePassword:  req.EnablePassword,
		CliList:         req.CliList,
		RetryFlag:       req.RetryFlag,
		TaskTimeout:     effTimeout,
		DeviceTimeout:   req.DeviceTimeout,
		Metadata:        map[string]interface{}{ "collect_mode": "fast" },
	}

	// 参数校验
	if err := h.validateCollectRequest(&r); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: err.Error()})
		return
	}

	// 调用采集服务：服务层已暂停任务写库；任务上下文在执行后移除，不保留记录
	resp, err := h.collectorService.ExecuteTask(c.Request.Context(), &r)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "EXEC_FAILED", Message: err.Error()})
		return
	}

	// 返回结果，关闭HTML转义以保留原始设备输出
	c.Header("Content-Type", "application/json")
	c.Status(http.StatusOK)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(gin.H{
		"code":    "SUCCESS",
		"message": "快速采集完成",
		"data":    resp,
	})
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

	// 使用自定义编码器关闭 HTML 转义，避免 \u003c/\u003e 转义影响原始设备输出可读性
	c.Header("Content-Type", "application/json")
	c.Status(http.StatusOK)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	encodeStart := time.Now()
	_ = enc.Encode(gin.H{
		"code":    "SUCCESS",
		"message": "批量任务执行完成",
		"data":    responses,
		"total":   len(responses),
	})
	encodeDur := time.Since(encodeStart)
	logger.Info("BatchExecute response encoded", "path", c.FullPath(), "size_bytes", c.Writer.Size(), "duration_ms", encodeDur.Milliseconds(), "count", len(responses))
}

// CustomerBatchRequest 自定义采集批量请求
type CustomerBatchRequest struct {
	TaskID      string           `json:"task_id"`
	TaskName    string           `json:"task_name,omitempty"`
	RetryFlag   *int             `json:"retry_flag,omitempty"`
	TaskTimeout *int             `json:"task_timeout,omitempty"`
	Devices     []CustomerDevice `json:"devices"`
}

// CustomerDevice 自定义采集设备参数
type CustomerDevice struct {
	DeviceIP        string   `json:"device_ip"`
	Port            int      `json:"device_port,omitempty"`
	DeviceName      string   `json:"device_name,omitempty"`
	DevicePlatform  string   `json:"device_platform,omitempty"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list,omitempty"`
	DeviceTimeout   *int     `json:"device_timeout,omitempty"`
}

// SystemBatchRequest 系统预制采集批量请求
type SystemBatchRequest struct {
	TaskID      string         `json:"task_id"`
	TaskName    string         `json:"task_name,omitempty"`
	RetryFlag   *int           `json:"retry_flag,omitempty"`
	TaskTimeout *int           `json:"task_timeout,omitempty"`
	DeviceList  []SystemDevice `json:"device_list"`
}

// SystemDevice 系统预制采集设备参数（cli_list 可选扩展）
type SystemDevice struct {
	DeviceIP        string   `json:"device_ip"`
	Port            int      `json:"device_port,omitempty"`
	DeviceName      string   `json:"device_name,omitempty"`
	DevicePlatform  string   `json:"device_platform"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list,omitempty"`
	DeviceTimeout   *int     `json:"device_timeout,omitempty"`
}

// BatchExecuteCustomer 自定义采集批量接口
// @Summary 自定义采集批量执行
// @Description 批量提交多个设备的自定义采集任务
// @Tags collector
// @Accept json
// @Produce json
// @Param request body CustomerBatchRequest true "自定义批量采集请求"
// @Success 200 {object} map[string]interface{} "批量采集结果，按设备组织"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/batch/custom [post]
func (h *CollectorHandler) BatchExecuteCustomer(c *gin.Context) {
	var req CustomerBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid custom batch request", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "请求参数无效: " + err.Error()})
		return
	}

	if strings.TrimSpace(req.TaskID) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "MISSING_TASK_ID", Message: "任务ID不能为空"})
		return
	}
	if len(req.Devices) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "EMPTY_DEVICES", Message: "设备列表不能为空"})
		return
	}
	if len(req.Devices) > 200 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "TOO_MANY_DEVICES", Message: "批量设备数量不能超过200"})
		return
	}

	// 基于服务的最大 worker 数控制批内并发度
	stats := h.collectorService.GetStats()
	maxWorkers := 4
	if v, ok := stats["max_workers"].(int); ok && v > 0 {
		maxWorkers = v
	}
	// 批次并发度不超过设备数量
	k := maxWorkers
	if k > len(req.Devices) {
		k = len(req.Devices)
	}
	if k <= 0 {
		k = 1
	}

	responses := make([]map[string]interface{}, len(req.Devices))
	reqCtx := c.Request.Context()
	sem := make(chan struct{}, k)
	g, ctx := errgroup.WithContext(reqCtx)

	for i, d := range req.Devices {
		i, d := i, d // capture loop vars
		g.Go(func() error {
			// 并发控制
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				// 请求已取消
				return nil
			}

			// 组装单设备请求（customer）
			r := service.CollectRequest{
				TaskID:          fmt.Sprintf("%s-%d", req.TaskID, i+1),
				TaskName:        req.TaskName,
				CollectOrigin:   "", // 已弃用，由路由决定采集模式
				DeviceIP:        d.DeviceIP,
				Port:            d.Port,
				DeviceName:      d.DeviceName,
				DevicePlatform:  d.DevicePlatform,
				CollectProtocol: d.CollectProtocol,
				UserName:        d.UserName,
				Password:        d.Password,
				EnablePassword:  d.EnablePassword,
				CliList:         d.CliList,
				RetryFlag:       req.RetryFlag,
				TaskTimeout:     req.TaskTimeout,
				DeviceTimeout:   d.DeviceTimeout,
				Metadata:        map[string]interface{}{"batch_task_id": req.TaskID, "collect_mode": "customer"},
			}

			if err := h.validateCollectRequest(&r); err != nil {
				responses[i] = map[string]interface{}{
					"device_ip":       d.DeviceIP,
					"port":            d.Port,
					"device_name":     d.DeviceName,
					"device_platform": d.DevicePlatform,
					"success":         false,
					"error":           "参数验证失败: " + err.Error(),
					"task_id":         r.TaskID,
					"timestamp":       time.Now(),
				}
				return nil
			}

			resp, err := h.collectorService.ExecuteTask(ctx, &r)
			if err != nil {
				resp = &service.CollectResponse{
					TaskID:    r.TaskID,
					Success:   false,
					Error:     err.Error(),
					Timestamp: time.Now(),
				}
			}

			responses[i] = map[string]interface{}{
				"device_ip":       d.DeviceIP,
				"port":            d.Port,
				"device_name":     d.DeviceName,
				"device_platform": d.DevicePlatform,
				"task_id":         resp.TaskID,
				"success":         resp.Success,
				"results":         resp.Results,
				"error":           resp.Error,
				"duration_ms":     resp.DurationMS,
				"timestamp":       resp.Timestamp,
			}
			return nil
		})
	}

	_ = g.Wait()

	// 汇总成功/失败以确定顶层返回码（与备份接口保持一致）
	successCount := 0
	for _, r := range responses {
		if s, ok := r["success"].(bool); ok && s {
			successCount++
		}
	}

	respCode := "SUCCESS"
	respMsg := "自定义批量任务执行完成"
	// 与备份接口对齐：全部或部分失败均返回 PARTIAL_SUCCESS，不返回 FAILED
	if successCount < len(responses) {
		// 包括 successCount == 0（全部失败）和部分成功
		respCode = "PARTIAL_SUCCESS"
		if successCount == 0 {
			respMsg = "自定义批量任务全部失败"
		} else {
			respMsg = "自定义批量任务部分成功"
		}
	}

	// 使用自定义编码器关闭 HTML 转义，避免 \u003c/\u003e 等转义影响原始输出可读性
	c.Header("Content-Type", "application/json")
	c.Status(http.StatusOK)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	encodeStart := time.Now()
	_ = enc.Encode(gin.H{
		"code":    respCode,
		"message": respMsg,
		"data":    responses,
		"total":   len(responses),
	})
	encodeDur := time.Since(encodeStart)
	logger.Info("BatchExecuteCustomer response encoded", "path", c.FullPath(), "size_bytes", c.Writer.Size(), "duration_ms", encodeDur.Milliseconds(), "count", len(responses))
}

// BatchExecuteSystem 系统预制采集批量接口
// @Summary 系统预制采集批量执行
// @Description 批量提交多个设备的系统预制采集任务
// @Tags collector
// @Accept json
// @Produce json
// @Param request body SystemBatchRequest true "系统预制批量采集请求"
// @Success 200 {object} map[string]interface{} "批量采集结果，按设备组织"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/collector/batch/system [post]
func (h *CollectorHandler) BatchExecuteSystem(c *gin.Context) {
	var req SystemBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Invalid system batch request", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "请求参数无效: " + err.Error()})
		return
	}

	if strings.TrimSpace(req.TaskID) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "MISSING_TASK_ID", Message: "任务ID不能为空"})
		return
	}
	if len(req.DeviceList) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "EMPTY_DEVICES", Message: "设备列表不能为空"})
		return
	}
	if len(req.DeviceList) > 200 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "TOO_MANY_DEVICES", Message: "批量设备数量不能超过200"})
		return
	}

	// 基于服务的最大 worker 数控制批内并发度
	stats := h.collectorService.GetStats()
	maxWorkers := 4
	if v, ok := stats["max_workers"].(int); ok && v > 0 {
		maxWorkers = v
	}
	k := maxWorkers
	if k > len(req.DeviceList) {
		k = len(req.DeviceList)
	}
	if k <= 0 {
		k = 1
	}

	responses := make([]map[string]interface{}, len(req.DeviceList))
	reqCtx := c.Request.Context()
	sem := make(chan struct{}, k)
	g, ctx := errgroup.WithContext(reqCtx)

	for i, d := range req.DeviceList {
		i, d := i, d // capture loop vars
		g.Go(func() error {
			// 并发控制
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return nil
			}

			// 校验平台必填
			if strings.TrimSpace(d.DevicePlatform) == "" {
				responses[i] = map[string]interface{}{
					"device_ip":       d.DeviceIP,
					"device_name":     d.DeviceName,
					"device_platform": d.DevicePlatform,
					"success":         false,
					"error":           "system模式需要指定设备平台(device_platform)",
					"task_id":         fmt.Sprintf("%s-%d", req.TaskID, i+1),
					"timestamp":       time.Now(),
				}
				return nil
			}

			// 仅使用用户提供的命令列表（不再注入平台默认命令）
			cliCombined := make([]string, 0, len(d.CliList))
			if len(d.CliList) > 0 {
				cliCombined = append(cliCombined, d.CliList...)
			}

			// 组装单设备请求（system）
			r := service.CollectRequest{
				TaskID:          fmt.Sprintf("%s-%d", req.TaskID, i+1),
				TaskName:        req.TaskName,
				CollectOrigin:   "", // 已弃用，由路由决定采集模式
				DeviceIP:        d.DeviceIP,
				Port:            d.Port,
				DeviceName:      d.DeviceName,
				DevicePlatform:  d.DevicePlatform,
				CollectProtocol: d.CollectProtocol,
				UserName:        d.UserName,
				Password:        d.Password,
				EnablePassword:  d.EnablePassword,
				CliList:         cliCombined, // 预组装系统命令 + 扩展命令
				RetryFlag:       req.RetryFlag,
				TaskTimeout:     req.TaskTimeout,
				DeviceTimeout:   d.DeviceTimeout,
				Metadata:        map[string]interface{}{"batch_task_id": req.TaskID, "collect_mode": "system"},
			}

			if err := h.validateCollectRequest(&r); err != nil {
				responses[i] = map[string]interface{}{
					"device_ip":       d.DeviceIP,
					"device_name":     d.DeviceName,
					"device_platform": d.DevicePlatform,
					"success":         false,
					"error":           "参数验证失败: " + err.Error(),
					"task_id":         r.TaskID,
					"timestamp":       time.Now(),
				}
				return nil
			}

			resp, err := h.collectorService.ExecuteTask(ctx, &r)
			if err != nil {
				resp = &service.CollectResponse{
					TaskID:    r.TaskID,
					Success:   false,
					Error:     err.Error(),
					Timestamp: time.Now(),
				}
			}

			responses[i] = map[string]interface{}{
				"device_ip":       d.DeviceIP,
				"port":            d.Port,
				"device_name":     d.DeviceName,
				"device_platform": d.DevicePlatform,
				"task_id":         resp.TaskID,
				"success":         resp.Success,
				"results":         resp.Results,
				"error":           resp.Error,
				"duration_ms":     resp.DurationMS,
				"timestamp":       resp.Timestamp,
			}
			return nil
		})
	}

	_ = g.Wait()

	// 汇总成功/失败以确定顶层返回码（与备份接口保持一致）
	successCount := 0
	for _, r := range responses {
		if s, ok := r["success"].(bool); ok && s {
			successCount++
		}
	}

	respCode := "SUCCESS"
	respMsg := "系统预制批量任务执行完成"
	if successCount < len(responses) {
		respCode = "PARTIAL_SUCCESS"
		if successCount == 0 {
			respMsg = "系统预制批量任务全部失败"
		} else {
			respMsg = "系统预制批量任务部分成功"
		}
	}

	// 使用自定义编码器关闭 HTML 转义，保持原始输出可读性（如 <, > 不被 \u003c/\u003e）
	c.Header("Content-Type", "application/json")
	c.Status(http.StatusOK)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	encodeStart := time.Now()
	_ = enc.Encode(gin.H{
		"code":    respCode,
		"message": respMsg,
		"data":    responses,
		"total":   len(responses),
	})
	encodeDur := time.Since(encodeStart)
	logger.Info("BatchExecuteSystem response encoded", "path", c.FullPath(), "size_bytes", c.Writer.Size(), "duration_ms", encodeDur.Milliseconds(), "count", len(responses))
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
	// 不再基于 origin 进行校验；平台校验在具体路由中处理
	// 端口（可选）范围校验
	if request.Port != 0 && (request.Port < 1 || request.Port > 65535) {
		return fmt.Errorf("端口号必须在1-65535之间")
	}
	// timeout 上限
	if request.TaskTimeout != nil && *request.TaskTimeout > 300 {
		return fmt.Errorf("任务超时时间不能超过300秒")
	}
	if request.DeviceTimeout != nil && *request.DeviceTimeout > 300 {
		return fmt.Errorf("设备超时时间不能超过300秒")
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

// GetCollectorSettings 获取快速采集设置（sqlite）
func (h *CollectorHandler) GetCollectorSettings(c *gin.Context) {
	db := database.GetDB()
	var s model.CollectorSettings
	if err := db.First(&s, 1).Error; err != nil {
		// 无记录时返回默认值
		c.JSON(http.StatusOK, gin.H{
			"code":    "SUCCESS",
			"message": "获取设置成功",
			"data": gin.H{
				"retry_flag": 0,
				"timeout":    30,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "获取设置成功",
		"data": gin.H{
			"retry_flag": s.RetryFlag,
			"timeout":    s.Timeout,
		},
	})
}

// UpdateCollectorSettings 保存快速采集设置（sqlite）
type UpdateCollectorSettingsRequest struct {
	RetryFlag int `json:"retry_flag"`
	Timeout   int `json:"timeout"`
}

func (h *CollectorHandler) UpdateCollectorSettings(c *gin.Context) {
	var req UpdateCollectorSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "请求参数无效: " + err.Error()})
		return
	}
	if req.RetryFlag < 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "retry_flag 不能为负数"})
		return
	}
	if req.Timeout <= 0 || req.Timeout > 300 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Code: "INVALID_PARAMS", Message: "timeout 必须在 1-300 秒之间"})
		return
	}
	s := model.CollectorSettings{ID: 1, RetryFlag: req.RetryFlag, Timeout: req.Timeout}
	err := database.WithRetry(func(db *gorm.DB) error {
		return db.Save(&s).Error
	}, 3, 100*time.Millisecond)
	if err != nil {
		logger.Error("Save CollectorSettings failed", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Code: "SAVE_FAILED", Message: "保存设置失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "保存设置成功",
		"data": gin.H{"retry_flag": s.RetryFlag, "timeout": s.Timeout},
	})
}
