package main

import (
    "context"
    "fmt"
    "net/http"
    "os"
    "os/signal"
    "strings"
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

    // 打印并发档位应用情况（按实际 workers 与 threads 输出）
    workers := cfg.Collector.Concurrent
    threads := cfg.Collector.Threads
    prof := strings.TrimSpace(cfg.Collector.ConcurrencyProfile)
    if prof != "" {
        logger.Info("Concurrency profile applied", "profile", prof, "workers", workers, "threads", threads)
    } else {
        logger.Info("Concurrency set by numeric value", "workers", workers, "threads", threads)
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

    // 创建备份服务
    backupService := service.NewBackupService(cfg)
    if err := backupService.Start(ctx); err != nil {
        logger.Fatal("Failed to start backup service", "error", err)
    }
    defer backupService.Stop()

    // 创建格式化服务
    formatService := service.NewFormatService(cfg)
    if err := formatService.Start(ctx); err != nil {
        logger.Fatal("Failed to start format service", "error", err)
    }
    defer formatService.Stop()

    // 创建部署服务（注入 CollectorService 以便编排前后采集）
    deployService := service.NewDeployService(cfg, collectorService)
    if err := deployService.Start(ctx); err != nil {
        logger.Fatal("Failed to start deploy service", "error", err)
    }
    defer deployService.Stop()

    // 设置路由
    r := router.SetupRouter(collectorService, backupService, formatService, deployService)

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