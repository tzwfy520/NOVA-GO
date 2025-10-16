package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/sshcollectorpro/sshcollectorpro/api/router"
    "github.com/sshcollectorpro/sshcollectorpro/internal/config"
    "github.com/sshcollectorpro/sshcollectorpro/internal/database"
    "github.com/sshcollectorpro/sshcollectorpro/internal/service"
    "github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

func main() {
	// 加载配置
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
    if err := logger.Init(logger.Config{
        Level:      cfg.Log.Level,
        Format:     cfg.Log.Format,
        Output:     cfg.Log.Output,
        FilePath:   cfg.Log.FilePath,
        MaxSize:    cfg.Log.MaxSize,
        MaxBackups: cfg.Log.MaxBackups,
        MaxAge:     cfg.Log.MaxAge,
        Compress:   cfg.Log.Compress,
    }); err != nil {
        fmt.Printf("Failed to initialize logger: %v\n", err)
        os.Exit(1)
    }

    logger.Info("Starting SSH Collector Pro Server", "version", "1.0.0")

    // 打印并发档位应用情况
    if cfg.Collector.ConcurrencyProfile != "" {
        logger.Info("Concurrency profile applied", "profile", cfg.Collector.ConcurrencyProfile, "workers", cfg.Collector.Concurrent)
    } else {
        logger.Info("Concurrency set by numeric value", "workers", cfg.Collector.Concurrent)
    }

	// 初始化数据库
	if err := database.InitSQLite(cfg.Database.SQLite); err != nil {
		logger.Fatal("Failed to initialize database", "error", err)
	}
	defer database.Close()

    // 已移除 Redis 依赖，直接运行

	// 创建采集器服务
	collectorService := service.NewCollectorService(cfg)
	ctx := context.Background()
	if err := collectorService.Start(ctx); err != nil {
		logger.Fatal("Failed to start collector service", "error", err)
	}
	defer collectorService.Stop()

	// 设置路由
	r := router.SetupRouter(collectorService)

	// 创建HTTP服务器
	server := &http.Server{
		Addr:           fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:        r,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	// 启动服务器
	go func() {
		logger.Info("Server starting", "port", cfg.Server.Port, "mode", cfg.Server.Mode)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", "error", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Server shutting down...")

	// 优雅关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err)
	} else {
		logger.Info("Server shutdown complete")
	}
}