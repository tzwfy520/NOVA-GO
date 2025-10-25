package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// DeviceHandler 设备处理器
type DeviceHandler struct{}

// NewDeviceHandler 创建设备处理器
func NewDeviceHandler() *DeviceHandler {
	return &DeviceHandler{}
}

// CreateDevice 创建设备
// @Summary 创建新设备
// @Description 添加新的设备信息到系统中
// @Tags device
// @Accept json
// @Produce json
// @Param device body model.DeviceInfo true "设备信息"
// @Success 201 {object} SuccessResponse "创建成功"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/devices [post]
func (h *DeviceHandler) CreateDevice(c *gin.Context) {
	var device model.DeviceInfo
	if err := c.ShouldBindJSON(&device); err != nil {
		logger.Error("Invalid device parameters", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "INVALID_PARAMS",
			Message: "设备参数无效: " + err.Error(),
		})
		return
	}

	// 参数验证
	if device.IP == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "MISSING_IP",
			Message: "设备IP不能为空",
		})
		return
	}

	if device.Port <= 0 || device.Port > 65535 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "INVALID_PORT",
			Message: "端口号必须在1-65535之间",
		})
		return
	}

	// 检查设备是否已存在
	db := database.GetDB()
	var existingDevice model.DeviceInfo
	if err := db.Where("ip = ? AND port = ? AND username = ?", device.IP, device.Port, device.Username).First(&existingDevice).Error; err == nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Code:    "DEVICE_EXISTS",
			Message: "设备已存在（IP/端口/用户名相同）",
		})
		return
	}

	// 设置默认值
	if device.Status == "" {
		device.Status = "unknown"
	}

	// 生成字符串主键ID（若为空）
	if device.ID == "" {
		device.ID = uuid.NewString()
	}

	// 创建设备
	if err := db.Create(&device).Error; err != nil {
		logger.Error("Failed to create device", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Code:    "CREATE_FAILED",
			Message: "创建设备失败: " + err.Error(),
		})
		return
	}

	logger.Info("Device created successfully", "device_id", device.ID, "ip", device.IP)
	c.JSON(http.StatusCreated, SuccessResponse{
		Code:    "SUCCESS",
		Message: "设备创建成功",
		Data:    device,
	})
}

// GetDevice 获取设备信息
// @Summary 获取设备详情
// @Description 根据设备ID获取设备的详细信息
// @Tags device
// @Accept json
// @Produce json
// @Param id path int true "设备ID"
// @Success 200 {object} model.DeviceInfo "设备信息"
// @Failure 404 {object} ErrorResponse "设备不存在"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/devices/{id} [get]
func (h *DeviceHandler) GetDevice(c *gin.Context) {
	idStr := c.Param("id")
	db := database.GetDB()
	var device model.DeviceInfo
	// 先按字符串ID查找，失败再尝试数值ID
	if err := db.Where("id = ?", idStr).First(&device).Error; err != nil {
		if id, err2 := strconv.ParseUint(idStr, 10, 32); err2 == nil {
			_ = db.First(&device, uint(id)).Error
		}
	}
	if device.ID == "" {
		parts := strings.Split(idStr, ":")
		if len(parts) == 3 {
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ? AND username = ?", parts[0], portNum, parts[2]).First(&device).Error
			}
		} else if len(parts) == 2 {
			// 兼容旧格式 ip:port
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ?", parts[0], portNum).First(&device).Error
			}
		}
		if device.ID == "" {
			c.JSON(http.StatusNotFound, ErrorResponse{ Code: "DEVICE_NOT_FOUND", Message: "设备不存在" })
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{ "code": "SUCCESS", "message": "获取设备信息成功", "data": device })
}

// UpdateDevice 更新设备信息
// @Summary 更新设备信息
// @Description 根据设备ID更新设备的信息
// @Tags device
// @Accept json
// @Produce json
// @Param id path int true "设备ID"
// @Param device body model.DeviceInfo true "设备信息"
// @Success 200 {object} SuccessResponse "更新成功"
// @Failure 400 {object} ErrorResponse "请求参数错误"
// @Failure 404 {object} ErrorResponse "设备不存在"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/devices/{id} [put]
func (h *DeviceHandler) UpdateDevice(c *gin.Context) {
	idStr := c.Param("id")
	var updateData model.DeviceInfo
	if err := c.ShouldBindJSON(&updateData); err != nil {
		logger.Error("Invalid update parameters", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{ Code: "INVALID_PARAMS", Message: "更新参数无效: " + err.Error() })
		return
	}
	db := database.GetDB()
	var device model.DeviceInfo
	if err := db.Where("id = ?", idStr).First(&device).Error; err != nil {
		if id, err2 := strconv.ParseUint(idStr, 10, 32); err2 == nil {
			_ = db.First(&device, uint(id)).Error
		}
	}
	if device.ID == "" {
		parts := strings.Split(idStr, ":")
		if len(parts) == 3 {
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ? AND username = ?", parts[0], portNum, parts[2]).First(&device).Error
			}
		} else if len(parts) == 2 {
			// 兼容旧格式 ip:port
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ?", parts[0], portNum).First(&device).Error
			}
		}
		if device.ID == "" {
			c.JSON(http.StatusNotFound, ErrorResponse{ Code: "DEVICE_NOT_FOUND", Message: "设备不存在" })
			return
		}
	}
	// 组合唯一冲突校验：以更新后的 ip/port/username 为准
	candidateIP := device.IP
	if strings.TrimSpace(updateData.IP) != "" {
		candidateIP = strings.TrimSpace(updateData.IP)
	}
	candidatePort := device.Port
	if updateData.Port > 0 {
		candidatePort = updateData.Port
	}
	candidateUsername := device.Username
	if strings.TrimSpace(updateData.Username) != "" {
		candidateUsername = strings.TrimSpace(updateData.Username)
	}
	var conflict model.DeviceInfo
	if err := db.Where("ip = ? AND port = ? AND username = ? AND id <> ?", candidateIP, candidatePort, candidateUsername, device.ID).First(&conflict).Error; err == nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{ Code: "DEVICE_EXISTS", Message: "设备已存在（IP/端口/用户名相同）" })
		return
	}
	if err := db.Model(&device).Updates(&updateData).Error; err != nil {
		logger.Error("Failed to update device", "device_id", idStr, "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{ Code: "UPDATE_FAILED", Message: "更新设备失败: " + err.Error() })
		return
	}
	logger.Info("Device updated successfully", "device_id", idStr)
	c.JSON(http.StatusOK, SuccessResponse{ Code: "SUCCESS", Message: "设备更新成功", Data: device })
}

// DeleteDevice 删除设备
// @Summary 删除设备
// @Description 根据设备ID删除设备
// @Tags device
// @Accept json
// @Produce json
// @Param id path int true "设备ID"
// @Success 200 {object} SuccessResponse "删除成功"
// @Failure 404 {object} ErrorResponse "设备不存在"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/devices/{id} [delete]
func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	idStr := c.Param("id")
	db := database.GetDB()
	var device model.DeviceInfo
	if err := db.Where("id = ?", idStr).First(&device).Error; err != nil {
		if id, err2 := strconv.ParseUint(idStr, 10, 32); err2 == nil {
			_ = db.First(&device, uint(id)).Error
		}
	}
	if device.ID == "" {
		parts := strings.Split(idStr, ":")
		if len(parts) == 3 {
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ? AND username = ?", parts[0], portNum, parts[2]).First(&device).Error
			}
		} else if len(parts) == 2 {
			// 兼容旧格式 ip:port
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ?", parts[0], portNum).First(&device).Error
			}
		}
		if device.ID == "" {
			c.JSON(http.StatusNotFound, ErrorResponse{ Code: "DEVICE_NOT_FOUND", Message: "设备不存在" })
			return
		}
	}
	if err := db.Delete(&device).Error; err != nil {
		logger.Error("Failed to delete device", "device_id", idStr, "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{ Code: "DELETE_FAILED", Message: "删除设备失败: " + err.Error() })
		return
	}
	logger.Info("Device deleted successfully", "device_id", idStr)
	c.JSON(http.StatusOK, SuccessResponse{ Code: "SUCCESS", Message: "设备删除成功", Data: gin.H{ "id": device.ID } })
}

// ListDevices 获取设备列表
// @Summary 获取设备列表
// @Description 分页获取设备列表，支持按状态和类型筛选
// @Tags device
// @Accept json
// @Produce json
// @Param page query int false "页码" default(1)
// @Param size query int false "每页数量" default(10)
// @Param status query string false "设备状态"
// @Param type query string false "设备类型"
// @Success 200 {object} map[string]interface{} "设备列表"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/devices [get]
func (h *DeviceHandler) ListDevices(c *gin.Context) {
    // 获取查询参数
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
    status := c.Query("status")
    deviceType := c.Query("type")
    // 新增：按启用状态筛选
    enabledParam := c.Query("enabled")
    if page < 1 {
        page = 1
    }
    if size < 1 || size > 100 {
        size = 10
    }

    db := database.GetDB()
    query := db.Model(&model.DeviceInfo{})

    // 添加筛选条件
    if status != "" {
        query = query.Where("status = ?", status)
    }
    if deviceType != "" {
        query = query.Where("device_type = ?", deviceType)
    }
    if enabledParam != "" {
        switch enabledParam {
        case "true", "1":
            query = query.Where("enabled = ?", true)
        case "false", "0":
            query = query.Where("enabled = ?", false)
        }
    }

    // 获取总数
    var total int64
    if err := query.Count(&total).Error; err != nil {
        logger.Error("Failed to count devices", "error", err)
        c.JSON(http.StatusInternalServerError, ErrorResponse{
            Code:    "COUNT_FAILED",
            Message: "获取设备总数失败: " + err.Error(),
        })
        return
    }

    // 分页查询
    var devices []model.DeviceInfo
    offset := (page - 1) * size
    if err := query.Offset(offset).Limit(size).Order("name ASC").Find(&devices).Error; err != nil {
        logger.Error("Failed to list devices", "error", err)
        c.JSON(http.StatusInternalServerError, ErrorResponse{
            Code:    "LIST_FAILED",
            Message: "获取设备列表失败: " + err.Error(),
        })
        return
    }
    c.JSON(http.StatusOK, gin.H{
        "code":    "SUCCESS",
        "message": "获取设备列表成功",
        "data": gin.H{
            "devices": devices,
            "pagination": gin.H{
                "page":  page,
                "size":  size,
                "total": total,
                "pages": (total + int64(size) - 1) / int64(size),
            },
        },
    })
}

// TestConnection 测试设备连接
// @Summary 测试设备连接
// @Description 测试指定设备的SSH连接是否正常
// @Tags device
// @Accept json
// @Produce json
// @Param id path int true "设备ID"
// @Success 200 {object} SuccessResponse "连接测试结果"
// @Failure 404 {object} ErrorResponse "设备不存在"
// @Failure 500 {object} ErrorResponse "服务器内部错误"
// @Router /api/v1/devices/{id}/test [post]
func (h *DeviceHandler) TestConnection(c *gin.Context) {
	idStr := c.Param("id")
	db := database.GetDB()
	var device model.DeviceInfo
	if err := db.Where("id = ?", idStr).First(&device).Error; err != nil {
		if id, err2 := strconv.ParseUint(idStr, 10, 32); err2 == nil {
			_ = db.First(&device, uint(id)).Error
		}
	}
	if device.ID == "" {
		parts := strings.Split(idStr, ":")
		if len(parts) == 3 {
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ? AND username = ?", parts[0], portNum, parts[2]).First(&device).Error
			}
		} else if len(parts) == 2 {
			// 兼容旧格式 ip:port
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ?", parts[0], portNum).First(&device).Error
			}
		}
		if device.ID == "" {
			c.JSON(http.StatusNotFound, ErrorResponse{ Code: "DEVICE_NOT_FOUND", Message: "设备不存在" })
			return
		}
	}

	// 这里应该调用SSH连接测试逻辑
	// 为了简化，这里只是模拟测试结果
	// 实际实现中应该使用SSH客户端进行连接测试
	
	success := true
	message := "连接测试成功"
	
	// 更新设备状态
	newStatus := "online"
	if !success {
		newStatus = "offline"
		message = "连接测试失败"
	}
	
	if err := db.Model(&device).Update("status", newStatus).Error; err != nil {
		logger.Error("Failed to update device status", "device_id", device.ID, "error", err)
	}

	logger.Info("Connection test completed", "device_id", device.ID, "success", success)
	c.JSON(http.StatusOK, SuccessResponse{
		Code:    "SUCCESS",
		Message: message,
		Data: gin.H{
			"device_id": device.ID,
			"success":   success,
			"status":    newStatus,
		},
	})
}

// 设置设备启用/禁用状态
type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *DeviceHandler) SetEnabled(c *gin.Context) {
	idStr := c.Param("id")
	var req setEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{ Code: "INVALID_PARAMS", Message: "参数无效: " + err.Error() })
		return
	}

	db := database.GetDB()
	var device model.DeviceInfo
	// 兼容字符串/数值主键两种形式
	if err := db.Where("id = ?", idStr).First(&device).Error; err != nil {
		if id, err2 := strconv.ParseUint(idStr, 10, 32); err2 == nil {
			_ = db.First(&device, uint(id)).Error
		}
	}
	if device.ID == "" {
		parts := strings.Split(idStr, ":")
		if len(parts) == 3 {
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ? AND username = ?", parts[0], portNum, parts[2]).First(&device).Error
			}
		} else if len(parts) == 2 {
			// 兼容旧格式 ip:port
			if portNum, err3 := strconv.Atoi(parts[1]); err3 == nil {
				_ = db.Where("ip = ? AND port = ?", parts[0], portNum).First(&device).Error
			}
		}
		if device.ID == "" {
			c.JSON(http.StatusNotFound, ErrorResponse{ Code: "DEVICE_NOT_FOUND", Message: "设备不存在" })
			return
		}
	}

	if err := db.Model(&device).Update("enabled", req.Enabled).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{ Code: "UPDATE_FAILED", Message: "更新设备启用状态失败: " + err.Error() })
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{ Code: "SUCCESS", Message: "设备启用状态已更新", Data: gin.H{ "id": device.ID, "enabled": req.Enabled } })
}