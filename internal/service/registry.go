package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/internal/database"
	"github.com/sshcollectorpro/sshcollectorpro/internal/model"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// RegistryService 注册服务
type RegistryService struct {
	config           *config.Config
	httpClient       *http.Client
	collector        *model.Collector
	collectorStatus  *model.CollectorStatus
	mutex            sync.RWMutex
	running          bool
	registerTicker   *time.Ticker
	heartbeatTicker  *time.Ticker
	stopChan         chan struct{}
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Version    string            `json:"version"`
	Tags       []string          `json:"tags"`
	ServerIP   string            `json:"server_ip"`
	ServerPort int               `json:"server_port"`
	Threads    int               `json:"threads"`
	Concurrent int               `json:"concurrent"`
	Metadata   map[string]string `json:"metadata"`
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		CollectorID string `json:"collector_id"`
		Config      struct {
			HeartbeatInterval int `json:"heartbeat_interval"`
			TaskTimeout       int `json:"task_timeout"`
		} `json:"config"`
	} `json:"data"`
}

// HeartbeatRequest 心跳请求
type HeartbeatRequest struct {
	CollectorID   string  `json:"collector_id"`
	Status        string  `json:"status"`
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsage   float64 `json:"memory_usage"`
	DiskUsage     float64 `json:"disk_usage"`
	TasksRunning  int     `json:"tasks_running"`
	TasksSuccess  int64   `json:"tasks_success"`
	TasksFailure  int64   `json:"tasks_failure"`
	LastHeartbeat int64   `json:"last_heartbeat"`
}

// HeartbeatResponse 心跳响应
type HeartbeatResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		NextHeartbeat int64 `json:"next_heartbeat"`
		Commands      []struct {
			Type   string                 `json:"type"`
			Params map[string]interface{} `json:"params"`
		} `json:"commands"`
	} `json:"data"`
}

// NewRegistryService 创建注册服务
func NewRegistryService(cfg *config.Config) *RegistryService {
	return &RegistryService{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		stopChan: make(chan struct{}),
	}
}

// Start 启动注册服务
func (s *RegistryService) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return fmt.Errorf("registry service is already running")
	}

	// 初始化采集器信息
	if err := s.initCollector(); err != nil {
		return fmt.Errorf("failed to initialize collector: %w", err)
	}

	// 启动注册和心跳
	s.running = true
	go s.registrationLoop(ctx)
	go s.heartbeatLoop(ctx)

	logger.Info("Registry service started", "collector_id", s.collector.ID)
	return nil
}

// Stop 停止注册服务
func (s *RegistryService) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	close(s.stopChan)

	if s.registerTicker != nil {
		s.registerTicker.Stop()
	}
	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
	}

	logger.Info("Registry service stopped")
	return nil
}

// initCollector 初始化采集器信息
func (s *RegistryService) initCollector() error {
	db := database.GetDB()

	// 查找或创建采集器记录
	s.collector = &model.Collector{
		ID:         s.config.Collector.ID,
		Type:       s.config.Collector.Type,
		Version:    s.config.Collector.Version,
		Tags:       strings.Join(s.config.Collector.Tags, ","),
		ServerIP:   s.getLocalIP(),
		ServerPort: s.config.Server.Port,
		Threads:    s.config.Collector.Threads,
		Concurrent: s.config.Collector.Concurrent,
		Status:     "starting",
	}

	// 尝试查找现有记录
	var existingCollector model.Collector
	if err := db.Where("id = ?", s.collector.ID).First(&existingCollector).Error; err == nil {
		// 更新现有记录
		s.collector.ID = existingCollector.ID
		if err := db.Model(&existingCollector).Updates(s.collector).Error; err != nil {
			return fmt.Errorf("failed to update collector: %w", err)
		}
		s.collector = &existingCollector
	} else {
		// 创建新记录
		if err := db.Create(s.collector).Error; err != nil {
			return fmt.Errorf("failed to create collector: %w", err)
		}
	}

	// 初始化采集器状态
	s.collectorStatus = &model.CollectorStatus{
		ID:               s.collector.ID,
		CPUUsage:         0.0,
		MemoryUsage:      0.0,
		DiskUsage:        0.0,
		TaskSuccessCount: 0,
		TaskFailureCount: 0,
		LastHeartbeat:    time.Now(),
	}

	// 查找或创建状态记录
	var existingStatus model.CollectorStatus
	if err := db.Where("collector_id = ?", s.collector.ID).First(&existingStatus).Error; err == nil {
		s.collectorStatus.ID = existingStatus.ID
		if err := db.Model(&existingStatus).Updates(s.collectorStatus).Error; err != nil {
			return fmt.Errorf("failed to update collector status: %w", err)
		}
		s.collectorStatus = &existingStatus
	} else {
		if err := db.Create(s.collectorStatus).Error; err != nil {
			return fmt.Errorf("failed to create collector status: %w", err)
		}
	}

	return nil
}

// registrationLoop 注册循环
func (s *RegistryService) registrationLoop(ctx context.Context) {
	// 立即尝试注册
	s.tryRegister()

	// 设置注册重试定时器
	s.registerTicker = time.NewTicker(s.config.Controller.RegisterInterval)
	defer s.registerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-s.registerTicker.C:
			if s.collector.Status != "online" {
				s.tryRegister()
			}
		}
	}
}

// heartbeatLoop 心跳循环
func (s *RegistryService) heartbeatLoop(ctx context.Context) {
	s.heartbeatTicker = time.NewTicker(s.config.Controller.HeartbeatInterval)
	defer s.heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-s.heartbeatTicker.C:
			if s.collector.Status == "online" {
				s.sendHeartbeat()
			}
		}
	}
}

// tryRegister 尝试注册
func (s *RegistryService) tryRegister() {
	logger.Info("Attempting to register collector", "collector_id", s.collector.ID)

	request := &RegisterRequest{
		ID:         s.collector.ID,
		Type:       s.collector.Type,
		Version:    s.collector.Version,
		Tags:       strings.Split(s.collector.Tags, ","),
		ServerIP:   s.collector.ServerIP,
		ServerPort: s.collector.ServerPort,
		Threads:    s.collector.Threads,
		Concurrent: s.collector.Concurrent,
		Metadata: map[string]string{
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
			"version": runtime.Version(),
		},
	}

	response, err := s.sendRegisterRequest(request)
	if err != nil {
		logger.Error("Failed to register collector", "error", err)
		s.updateCollectorStatus("offline")
		return
	}

	if response.Success {
		logger.Info("Collector registered successfully", "collector_id", s.collector.ID)
		s.updateCollectorStatus("online")
	} else {
		logger.Error("Registration failed", "message", response.Message)
		s.updateCollectorStatus("offline")
	}
}

// sendHeartbeat 发送心跳
func (s *RegistryService) sendHeartbeat() {
	// 更新系统状态
	s.updateSystemStats()

	request := &HeartbeatRequest{
		CollectorID:   s.collector.ID,
		Status:        s.collector.Status,
		CPUUsage:      s.collectorStatus.CPUUsage,
		MemoryUsage:   s.collectorStatus.MemoryUsage,
		DiskUsage:     s.collectorStatus.DiskUsage,
		TasksRunning:  0, // 当前运行任务数，需要从其他地方获取
		TasksSuccess:  s.collectorStatus.TaskSuccessCount,
		TasksFailure:  s.collectorStatus.TaskFailureCount,
		LastHeartbeat: time.Now().Unix(),
	}

	response, err := s.sendHeartbeatRequest(request)
	if err != nil {
		logger.Error("Failed to send heartbeat", "error", err)
		s.updateCollectorStatus("offline")
		return
	}

	if response.Success {
		logger.Debug("Heartbeat sent successfully")
		s.collectorStatus.LastHeartbeat = time.Now()
		s.updateCollectorStatusInDB()
	} else {
		logger.Error("Heartbeat failed", "message", response.Message)
		s.updateCollectorStatus("offline")
	}
}

// sendRegisterRequest 发送注册请求
func (s *RegistryService) sendRegisterRequest(request *RegisterRequest) (*RegisterResponse, error) {
	url := fmt.Sprintf("http://%s/api/v1/collectors/register", s.config.GetControllerAddr())
	
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := s.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var response RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// sendHeartbeatRequest 发送心跳请求
func (s *RegistryService) sendHeartbeatRequest(request *HeartbeatRequest) (*HeartbeatResponse, error) {
	url := fmt.Sprintf("http://%s/api/v1/collectors/%s/heartbeat", s.config.GetControllerAddr(), s.collector.ID)
	
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := s.httpClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var response HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// updateCollectorStatus 更新采集器状态
func (s *RegistryService) updateCollectorStatus(status string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.collector.Status = status
	s.updateCollectorStatusInDB()
}

// updateCollectorStatusInDB 更新数据库中的状态
func (s *RegistryService) updateCollectorStatusInDB() {
	db := database.GetDB()
	
	// 更新采集器状态
	if err := db.Model(s.collector).Update("status", s.collector.Status).Error; err != nil {
		logger.Error("Failed to update collector status", "error", err)
	}

	// 更新采集器详细状态
	if err := db.Model(s.collectorStatus).Updates(s.collectorStatus).Error; err != nil {
		logger.Error("Failed to update collector detailed status", "error", err)
	}
}

// updateSystemStats 更新系统统计信息
func (s *RegistryService) updateSystemStats() {
	// 这里应该实现真实的系统监控逻辑
	// 为了简化，使用模拟数据
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	s.collectorStatus.CPUUsage = 15.5      // 模拟CPU使用率
	s.collectorStatus.MemoryUsage = float64(m.Alloc) / 1024 / 1024 // MB
	s.collectorStatus.DiskUsage = 45.2     // 模拟磁盘使用率
	// TasksRunning 需要从CollectorService获取，这里暂时不设置
}

// getLocalIP 获取本地IP地址
func (s *RegistryService) getLocalIP() string {
	// 简化实现，返回配置的主机地址或默认值
	if s.config.Server.Host != "" && s.config.Server.Host != "0.0.0.0" {
		return s.config.Server.Host
	}
	return "127.0.0.1"
}

// GetCollector 获取采集器信息
func (s *RegistryService) GetCollector() *model.Collector {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.collector
}

// GetCollectorStatus 获取采集器状态
func (s *RegistryService) GetCollectorStatus() *model.CollectorStatus {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.collectorStatus
}

// IsOnline 检查采集器是否在线
func (s *RegistryService) IsOnline() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.collector != nil && s.collector.Status == "online"
}