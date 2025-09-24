package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/cache"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// CollectorService 采集器服务
type CollectorService struct {
	config   *config.Config
	sshPool  *ssh.Pool
	mutex    sync.RWMutex
	running  bool
	tasks    map[string]*TaskContext
	workers  chan struct{}
}

// TaskContext 任务上下文
type TaskContext struct {
	Task      *model.Task
	Cancel    context.CancelFunc
	StartTime time.Time
	Status    string
}

// CollectRequest 采集请求
type CollectRequest struct {
	TaskID    string                 `json:"task_id"`
	DeviceIP  string                 `json:"device_ip"`
	Port      int                    `json:"port"`
	Username  string                 `json:"username"`
	Password  string                 `json:"password"`
	Commands  []string               `json:"commands"`
	Timeout   int                    `json:"timeout"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// CollectResponse 采集响应
type CollectResponse struct {
	TaskID    string                 `json:"task_id"`
	Success   bool                   `json:"success"`
	Results   []*ssh.CommandResult   `json:"results"`
	Error     string                 `json:"error"`
	Duration  time.Duration          `json:"duration"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// NewCollectorService 创建采集器服务
func NewCollectorService(cfg *config.Config) *CollectorService {
	// 创建SSH连接池配置
	poolConfig := &ssh.PoolConfig{
		MaxIdle:     10,
		MaxActive:   cfg.Collector.Concurrent,
		IdleTimeout: 5 * time.Minute,
		SSHConfig: &ssh.Config{
			Timeout:     cfg.SSH.Timeout,
			KeepAlive:   cfg.SSH.KeepAliveInterval,
			MaxSessions: cfg.SSH.MaxSessions,
		},
	}

	return &CollectorService{
		config:  cfg,
		sshPool: ssh.NewPool(poolConfig),
		tasks:   make(map[string]*TaskContext),
		workers: make(chan struct{}, cfg.Collector.Concurrent),
	}
}

// Start 启动采集器服务
func (s *CollectorService) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return fmt.Errorf("collector service is already running")
	}

	s.running = true
	logger.Info("Collector service started")

	// 启动任务清理协程
	go s.cleanupTasks(ctx)

	return nil
}

// Stop 停止采集器服务
func (s *CollectorService) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	// 取消所有正在运行的任务
	for _, taskCtx := range s.tasks {
		if taskCtx.Cancel != nil {
			taskCtx.Cancel()
		}
	}

	// 关闭SSH连接池
	if err := s.sshPool.Close(); err != nil {
		logger.Error("Failed to close SSH pool", "error", err)
	}

	logger.Info("Collector service stopped")
	return nil
}

// ExecuteTask 执行采集任务
func (s *CollectorService) ExecuteTask(ctx context.Context, request *CollectRequest) (*CollectResponse, error) {
	if !s.running {
		return nil, fmt.Errorf("collector service is not running")
	}

	// 获取工作协程
	select {
	case s.workers <- struct{}{}:
		defer func() { <-s.workers }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	startTime := time.Now()
	response := &CollectResponse{
		TaskID:    request.TaskID,
		Timestamp: startTime,
		Metadata:  request.Metadata,
	}

	// 创建任务记录
	task := &model.Task{
		ID:          request.TaskID,
		CollectorID: s.config.Collector.ID,
		Type:        model.TaskTypeSimple,
		DeviceIP:    request.DeviceIP,
		DevicePort:  request.Port,
		Username:    request.Username,
		Password:    request.Password,
		Commands:    strings.Join(request.Commands, ";"),
		Status:      model.TaskStatusRunning,
		CreatedAt:   startTime,
		UpdatedAt:   startTime,
	}

	// 保存任务到数据库
	if err := s.saveTask(task); err != nil {
		logger.Error("Failed to save task", "task_id", request.TaskID, "error", err)
	}

	// 创建任务上下文
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Timeout)*time.Second)
	defer cancel()

	s.addTaskContext(request.TaskID, &TaskContext{
		Task:      task,
		Cancel:    cancel,
		StartTime: startTime,
		Status:    "running",
	})
	defer s.removeTaskContext(request.TaskID)

	// 执行SSH采集
	results, err := s.executeSSHCollection(taskCtx, request)
	response.Duration = time.Since(startTime)

	if err != nil {
		response.Success = false
		response.Error = err.Error()
		task.Status = model.TaskStatusFailed
		task.ErrorMsg = err.Error()
		
		// 记录错误日志
		s.logTaskError(request.TaskID, err.Error())
	} else {
		response.Success = true
		response.Results = results
		task.Status = model.TaskStatusSuccess
		
		// 序列化结果
		if resultData, err := json.Marshal(results); err == nil {
			task.Result = string(resultData)
		}
	}

	// 更新任务状态
	task.Duration = int64(response.Duration.Seconds())
	task.UpdatedAt = time.Now()
	if err := s.updateTask(task); err != nil {
		logger.Error("Failed to update task", "task_id", request.TaskID, "error", err)
	}

	// 缓存结果
	s.cacheResult(request.TaskID, response)

	return response, nil
}

// executeSSHCollection 执行SSH采集
func (s *CollectorService) executeSSHCollection(ctx context.Context, request *CollectRequest) ([]*ssh.CommandResult, error) {
	// 创建SSH连接信息
	connInfo := &ssh.ConnectionInfo{
		Host:     request.DeviceIP,
		Port:     request.Port,
		Username: request.Username,
		Password: request.Password,
	}

	// 记录开始日志
	s.logTaskInfo(request.TaskID, fmt.Sprintf("Starting SSH collection for %s:%d", request.DeviceIP, request.Port))

	// 执行命令
	results, err := s.sshPool.ExecuteCommands(ctx, connInfo, request.Commands)
	if err != nil {
		s.logTaskError(request.TaskID, fmt.Sprintf("SSH execution failed: %v", err))
		return nil, fmt.Errorf("SSH execution failed: %w", err)
	}

	// 记录成功日志
	s.logTaskInfo(request.TaskID, fmt.Sprintf("SSH collection completed, executed %d commands", len(results)))

	return results, nil
}

// GetTaskStatus 获取任务状态
func (s *CollectorService) GetTaskStatus(taskID string) (*TaskContext, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if taskCtx, exists := s.tasks[taskID]; exists {
		return taskCtx, nil
	}

	return nil, fmt.Errorf("task not found: %s", taskID)
}

// CancelTask 取消任务
func (s *CollectorService) CancelTask(taskID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if taskCtx, exists := s.tasks[taskID]; exists {
		if taskCtx.Cancel != nil {
			taskCtx.Cancel()
			taskCtx.Status = "cancelled"
		}
		return nil
	}

	return fmt.Errorf("task not found: %s", taskID)
}

// GetStats 获取采集器统计信息
func (s *CollectorService) GetStats() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := map[string]interface{}{
		"running":       s.running,
		"active_tasks":  len(s.tasks),
		"max_workers":   cap(s.workers),
		"busy_workers":  len(s.workers),
		"ssh_pool":      s.sshPool.GetStats(),
	}

	return stats
}

// addTaskContext 添加任务上下文
func (s *CollectorService) addTaskContext(taskID string, taskCtx *TaskContext) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.tasks[taskID] = taskCtx
}

// removeTaskContext 移除任务上下文
func (s *CollectorService) removeTaskContext(taskID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.tasks, taskID)
}

// cleanupTasks 清理过期任务
func (s *CollectorService) cleanupTasks(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpiredTasks()
		}
	}
}

// cleanupExpiredTasks 清理过期任务
func (s *CollectorService) cleanupExpiredTasks() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now()
	toDelete := make([]string, 0)

	for taskID, taskCtx := range s.tasks {
		// 清理超过1小时的任务
		if now.Sub(taskCtx.StartTime) > time.Hour {
			toDelete = append(toDelete, taskID)
		}
	}

	for _, taskID := range toDelete {
		delete(s.tasks, taskID)
	}
}

// saveTask 保存任务到数据库
func (s *CollectorService) saveTask(task *model.Task) error {
	db := database.GetDB()
	return db.Create(task).Error
}

// updateTask 更新任务状态
func (s *CollectorService) updateTask(task *model.Task) error {
	db := database.GetDB()
	return db.Save(task).Error
}

// cacheResult 缓存结果到Redis
func (s *CollectorService) cacheResult(taskID string, response *CollectResponse) {
	redis := cache.GetRedis()
	if redis == nil {
		return
	}

	data, err := json.Marshal(response)
	if err != nil {
		logger.Error("Failed to marshal response", "task_id", taskID, "error", err)
		return
	}

	key := fmt.Sprintf("task_result:%s", taskID)
	if err := redis.Set(context.Background(), key, data, 24*time.Hour).Err(); err != nil {
		logger.Error("Failed to cache result", "task_id", taskID, "error", err)
	}
}

// logTaskInfo 记录任务信息日志
func (s *CollectorService) logTaskInfo(taskID, message string) {
	logger.Info("Task info", "task_id", taskID, "message", message)
	s.saveTaskLog(taskID, "INFO", message)
}

// logTaskError 记录任务错误日志
func (s *CollectorService) logTaskError(taskID, message string) {
	logger.Error("Task error", "task_id", taskID, "message", message)
	s.saveTaskLog(taskID, "ERROR", message)
}

// saveTaskLog 保存任务日志
func (s *CollectorService) saveTaskLog(taskID, level, message string) {
	db := database.GetDB()
	taskLog := &model.TaskLog{
		TaskID:  taskID,
		Level:   level,
		Message: message,
	}
	
	if err := db.Create(taskLog).Error; err != nil {
		logger.Error("Failed to save task log", "task_id", taskID, "error", err)
	}
}