package router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sshcollectorpro/sshcollectorpro/api/handler"
	"github.com/sshcollectorpro/sshcollectorpro/internal/service"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// SetupRouter 设置路由
func SetupRouter(collectorService *service.CollectorService, backupService *service.BackupService, formatService *service.FormatService, deployService *service.DeployService) *gin.Engine {
	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 创建路由引擎
	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(CORSMiddleware())
	r.Use(RequestIDMiddleware())
	r.Use(LoggingMiddleware())

	// 静态资源与管理页入口
	r.Static("/static", "./web/static")
	r.GET("/admin", func(c *gin.Context) {
		c.File("web/admin.html")
	})
	// 新增独立功能页面入口
	r.GET("/admin/collector", func(c *gin.Context) { c.File("web/admin/collector.html") })
	r.GET("/admin/device-types", func(c *gin.Context) { c.File("web/admin/device_types.html") })
	r.GET("/admin/devices", func(c *gin.Context) { c.File("web/admin/devices.html") })
	r.GET("/admin/ssh-adapter", func(c *gin.Context) { c.File("web/admin/ssh_adapter.html") })
	r.GET("/admin/logs", func(c *gin.Context) { c.File("web/admin/logs.html") })
	r.GET("/admin/quick-collect", func(c *gin.Context) { c.File("web/admin/quick_collect.html") })
	r.GET("/admin/simulate", func(c *gin.Context) { c.File("web/admin/simulate.html") })
	r.GET("/admin/simulate-data", func(c *gin.Context) { c.File("web/admin/simulate_data.html") })

	// 创建处理器
	collectorHandler := handler.NewCollectorHandler(collectorService)
	deviceHandler := handler.NewDeviceHandler()
	backupHandler := handler.NewBackupHandler(backupService)
	formattedHandler := handler.NewFormattedHandler(formatService)
	deployHandler := handler.NewDeployHandler(deployService)
	adminHandler := handler.NewAdminHandler()
	simCmdHandler := handler.NewSimCmdHandler()
	simDeviceCmdHandler := handler.NewSimDeviceCmdHandler()
	logsHandler := handler.NewLogsHandler()
	sshAdapterHandler := handler.NewSSHAdapterHandler()
	simulateConfigHandler := handler.NewSimulateConfigHandler()

	// 根路径
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":    "SSH Collector Pro",
			"version": "1.0.0",
			"status":  "running",
		})
	})

	// API v1 路由组
	v1 := r.Group("/api/v1")
	{
		// 健康检查
		v1.GET("/health", collectorHandler.Health)

		// 采集器相关路由
		collector := v1.Group("/collector")
		{
			collector.POST("/batch", collectorHandler.BatchExecute)
			// 新增拆封后的批量接口
			collector.POST("/batch/custom", collectorHandler.BatchExecuteCustomer)
			collector.POST("/batch/system", collectorHandler.BatchExecuteSystem)
			collector.GET("/task/:task_id/status", collectorHandler.GetTaskStatus)
			collector.POST("/task/:task_id/cancel", collectorHandler.CancelTask)
			collector.GET("/stats", collectorHandler.GetStats)
		}

		// 设备管理路由
		devices := v1.Group("/devices")
		{
			devices.POST("", deviceHandler.CreateDevice)
			devices.GET("", deviceHandler.ListDevices)
			devices.GET("/:id", deviceHandler.GetDevice)
			devices.PUT("/:id", deviceHandler.UpdateDevice)
			devices.DELETE("/:id", deviceHandler.DeleteDevice)
			devices.POST("/:id/test", deviceHandler.TestConnection)
		}

		// 备份路由
		v1.POST("/backup/batch", backupHandler.BatchBackup)

		// 数据格式化路由
		formatted := v1.Group("/formatted")
		{
			formatted.POST("/batch", formattedHandler.BatchFormatted)
			formatted.POST("/fast", formattedHandler.FastFormatted)
		}

		// 部署路由
		v1.POST("/deploy/fast", deployHandler.FastDeploy)

		// 管理路由：设备类型默认参数
		admin := v1.Group("/admin")
		{
			admin.GET("/device-defaults", adminHandler.GetDeviceDefaults)
			admin.PUT("/device-defaults/:platform", adminHandler.UpdateDeviceDefaults)
		}

		// SSH适配管理
		ssh := v1.Group("/ssh-adapter")
		{
			ssh.GET("/platforms", sshAdapterHandler.ListPlatforms)
			ssh.POST("/platforms", sshAdapterHandler.CreatePlatform)
			ssh.GET("/platforms/:id", sshAdapterHandler.GetPlatform)
			ssh.PUT("/platforms/:id", sshAdapterHandler.UpdatePlatform)
			ssh.DELETE("/platforms/:id", sshAdapterHandler.DeletePlatform)
			ssh.GET("/platforms/:id/params", sshAdapterHandler.GetParams)
			ssh.PUT("/platforms/:id/params", sshAdapterHandler.UpdateParams)
			ssh.GET("/platforms/:id/yaml", sshAdapterHandler.GetPlatformYAML)
			ssh.POST("/generate", sshAdapterHandler.GenerateYAML)
		}

		// 模拟命令管理
		sim := v1.Group("/simcmds")
		{
			sim.GET("", simCmdHandler.ListSimCmds)
			sim.POST("", simCmdHandler.CreateSimCmd)
			sim.PUT("/:id", simCmdHandler.UpdateSimCmd)
			sim.DELETE("/:id", simCmdHandler.DeleteSimCmd)
		}

		// 模拟数据（按命名空间与设备）管理
		simdev := v1.Group("/sim-device-cmds")
		{
			simdev.GET("", simDeviceCmdHandler.ListSimDeviceCmds)
			simdev.POST("", simDeviceCmdHandler.CreateSimDeviceCmd)
			simdev.GET("/:id", simDeviceCmdHandler.GetSimDeviceCmd)
			simdev.PUT("/:id", simDeviceCmdHandler.UpdateSimDeviceCmd)
			simdev.DELETE("/:id", simDeviceCmdHandler.DeleteSimDeviceCmd)
		}

		// 模拟配置管理
		simcfg := v1.Group("/simulate-config")
		{
			simcfg.GET("", simulateConfigHandler.GetSimulateConfig)
			simcfg.POST("", simulateConfigHandler.SaveSimulateConfig)
		}

		// 兼容前端已存在路径：/simulate/config
		v1.GET("/simulate/config", simulateConfigHandler.GetSimulateConfig)
		v1.POST("/simulate/config", simulateConfigHandler.SaveSimulateConfig)

		// 日志查询
		v1.GET("/logs/tail", logsHandler.TailLogs)
	}

	// 404处理
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    "NOT_FOUND",
			"message": "接口不存在",
			"path":    c.Request.URL.Path,
		})
	})

	return r
}

// CORSMiddleware 跨域中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Request-ID")
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// RequestIDMiddleware 请求ID中间件
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	}
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录请求开始时间
		start := time.Now()

		// 处理请求
		c.Next()

		// 计算处理时间
		duration := time.Since(start)

		// 获取请求信息
		requestID := c.GetString("request_id")
		method := c.Request.Method
		path := c.Request.URL.Path
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		// 记录日志
		logger.Info("HTTP Request",
			"request_id", requestID,
			"method", method,
			"path", path,
			"status", statusCode,
			"duration", duration,
			"client_ip", clientIP,
			"user_agent", userAgent,
		)

		// 如果是错误状态码，记录错误日志
		if statusCode >= 400 {
			logger.Error("HTTP Error",
				"request_id", requestID,
				"method", method,
				"path", path,
				"status", statusCode,
				"duration", duration,
				"client_ip", clientIP,
			)
		}
	}
}

// generateRequestID 生成请求ID
func generateRequestID() string {
	// 简单的请求ID生成，实际项目中可以使用UUID
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
