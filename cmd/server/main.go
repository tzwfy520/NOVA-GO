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

	"github.com/fsnotify/fsnotify"

	"github.com/sshcollectorpro/sshcollectorpro/api/router"
	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/service"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"github.com/sshcollectorpro/sshcollectorpro/simulate"
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

	// 启动模拟服务（可选）
	var simMgr *simulate.Manager
	if cfg.Server.SimulateEnable {
		simPath := "simulate/simulate.yaml"
		if _, err := os.Stat(simPath); err != nil {
			logger.Warn("Simulate: simulate.yaml missing, skip starting simulate servers", "path", simPath, "error", err)
		} else {
			sc, err := simulate.LoadConfig(simPath)
			if err != nil {
				logger.Warn("Simulate: failed to load simulate.yaml", "error", err)
			} else {
				mgr, err := simulate.Start(sc)
				if err != nil {
					logger.Warn("Simulate: failed to start", "error", err)
				} else {
					simMgr = mgr
					logger.Info("Simulate: started", "namespaces", len(sc.Namespace))
					// 汇总输出所有命名空间端口，便于快速确认
					ports := make([]string, 0, len(sc.Namespace))
					for ns, nsCfg := range sc.Namespace {
						ports = append(ports, fmt.Sprintf("%s:%d", ns, nsCfg.Port))
					}
					logger.Info("Simulate: ports enabled", "ports", strings.Join(ports, ", "))
				}
			}
		}
	}
	defer func() {
		if simMgr != nil {
			simMgr.Stop()
		}
	}()

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

	// 配置文件监听与热更新
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			logger.Warn("Config watch init failed", "error", err)
			return
		}
		defer watcher.Close()
		path := "configs/config.yaml"
		if err := watcher.Add(path); err != nil {
			logger.Warn("Config watch add failed", "error", err)
			return
		}
		var debounce *time.Timer
		debounceInterval := 300 * time.Millisecond
		trigger := func() {
			newCfg, err := config.Load(path)
			if err != nil {
				logger.Warn("Config reload failed", "error", err)
				return
			}
			// 原地覆盖，保持指针不变
			*cfg = *newCfg
			// 刷新日志配置
			_ = logger.Init(logger.Config{
				Level:      cfg.Log.Level,
				Format:     cfg.Log.Format,
				Output:     cfg.Log.Output,
				FilePath:   cfg.Log.FilePath,
				MaxSize:    cfg.Log.MaxSize,
				MaxBackups: cfg.Log.MaxBackups,
				MaxAge:     cfg.Log.MaxAge,
				Compress:   cfg.Log.Compress,
			})
			logger.Info("Config reloaded")
			// 模拟开关变化时动态启停
			if cfg.Server.SimulateEnable && simMgr == nil {
				simPath := "simulate/simulate.yaml"
				sc, err := simulate.LoadConfig(simPath)
				if err != nil {
					logger.Warn("Simulate: failed to load simulate.yaml on config reload", "error", err)
				} else {
					mgr, err := simulate.Start(sc)
					if err != nil {
						logger.Warn("Simulate: failed to start on config reload", "error", err)
					} else {
						simMgr = mgr
						logger.Info("Simulate: started by config reload")
					}
				}
			} else if !cfg.Server.SimulateEnable && simMgr != nil {
				simMgr.Stop()
				simMgr = nil
				logger.Info("Simulate: stopped by config reload")
			}
		}
		for {
			select {
			case ev := <-watcher.Events:
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					if debounce != nil {
						debounce.Stop()
					}
					debounce = time.AfterFunc(debounceInterval, trigger)
				}
			case err := <-watcher.Errors:
				logger.Warn("Config watch error", "error", err)
			}
		}
	}()

	// simulate.yaml 监听与热更新
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			logger.Warn("Simulate watch init failed", "error", err)
			return
		}
		defer watcher.Close()
		path := "simulate/simulate.yaml"
		if _, err := os.Stat(path); err != nil {
			logger.Warn("Simulate: simulate.yaml not found, skip watch", "error", err)
			return
		}
		if err := watcher.Add(path); err != nil {
			logger.Warn("Simulate watch add failed", "error", err)
			return
		}
		var debounce *time.Timer
		debounceInterval := 300 * time.Millisecond
		trigger := func() {
			sc, err := simulate.LoadConfig(path)
			if err != nil {
				logger.Warn("Simulate: reload simulate.yaml failed", "error", err)
				return
			}
			if !cfg.Server.SimulateEnable {
				logger.Info("Simulate: reload ignored, simulate disabled")
				return
			}
			if simMgr == nil {
				mgr, err := simulate.Start(sc)
				if err != nil {
					logger.Warn("Simulate: start failed on simulate reload", "error", err)
					return
				}
				simMgr = mgr
				logger.Info("Simulate: started by simulate reload")
			} else {
				if err := simMgr.Reload(sc); err != nil {
					logger.Warn("Simulate: hot reload failed", "error", err)
				} else {
					logger.Info("Simulate: hot reload success")
				}
			}
		}
		for {
			select {
			case ev := <-watcher.Events:
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					if debounce != nil {
						debounce.Stop()
					}
					debounce = time.AfterFunc(debounceInterval, trigger)
				}
			case err := <-watcher.Errors:
				logger.Warn("Simulate watch error", "error", err)
			}
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
