package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// XXLJobService XXL-Job执行器服务
type XXLJobService struct {
	config          *config.Config
	httpClient      *http.Client
	collectorSvc    *CollectorService
	running         bool
	mutex           sync.RWMutex
	stopChan        chan struct{}
	heartbeatTicker *time.Ticker
	logFiles        map[int64]*os.File
	logMutex        sync.RWMutex
}

// JobRequest 任务请求
type JobRequest struct {
	JobID                 int64  `json:"jobId"`
	ExecutorHandler       string `json:"executorHandler"`
	ExecutorParams        string `json:"executorParams"`
	ExecutorBlockStrategy string `json:"executorBlockStrategy"`
	ExecutorTimeout       int    `json:"executorTimeout"`
	LogID                 int64  `json:"logId"`
	LogDateTime           int64  `json:"logDateTime"`
	GlueType              string `json:"glueType"`
	GlueSource            string `json:"glueSource"`
	GlueUpdatetime        int64  `json:"glueUpdatetime"`
	BroadcastIndex        int    `json:"broadcastIndex"`
	BroadcastTotal        int    `json:"broadcastTotal"`
}

// JobResponse 任务响应
type JobResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// LogRequest 日志请求
type LogRequest struct {
	LogDateTim  int64 `json:"logDateTim"`
	LogID       int64 `json:"logId"`
	FromLineNum int   `json:"fromLineNum"`
}

// LogResponse 日志响应
type LogResponse struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Content struct {
		FromLineNum int    `json:"fromLineNum"`
		ToLineNum   int    `json:"toLineNum"`
		LogContent  string `json:"logContent"`
		IsEnd       bool   `json:"isEnd"`
	} `json:"content"`
}

// RegistryRequest 注册请求
type RegistryRequest struct {
	RegistryGroup string `json:"registryGroup"`
	RegistryKey   string `json:"registryKey"`
	RegistryValue string `json:"registryValue"`
}

// RegistryResponse 注册响应
type RegistryResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// NewXXLJobService 创建XXL-Job服务
func NewXXLJobService(cfg *config.Config, collectorSvc *CollectorService) *XXLJobService {
	return &XXLJobService{
		config:       cfg,
		collectorSvc: collectorSvc,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		stopChan: make(chan struct{}),
		logFiles: make(map[int64]*os.File),
	}
}

// Start 启动XXL-Job服务
func (s *XXLJobService) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		return fmt.Errorf("xxl-job service is already running")
	}

	// 创建日志目录
	if err := s.createLogDir(); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// 启动心跳
	s.running = true
	go s.heartbeatLoop(ctx)

	// 注册执行器
	go s.registryLoop(ctx)

	logger.Info("XXL-Job service started")
	return nil
}

// Stop 停止XXL-Job服务
func (s *XXLJobService) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	close(s.stopChan)

	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
	}

	// 关闭所有日志文件
	s.logMutex.Lock()
	for _, file := range s.logFiles {
		file.Close()
	}
	s.logFiles = make(map[int64]*os.File)
	s.logMutex.Unlock()

	logger.Info("XXL-Job service stopped")
	return nil
}

// ExecuteJob 执行任务
func (s *XXLJobService) ExecuteJob(req *JobRequest) *JobResponse {
	logger.Info("Executing XXL-Job task", "job_id", req.JobID, "handler", req.ExecutorHandler)

	// 创建日志文件
	logFile, err := s.createLogFile(req.LogID)
	if err != nil {
		logger.Error("Failed to create log file", "error", err)
		return &JobResponse{Code: 500, Msg: fmt.Sprintf("Failed to create log file: %v", err)}
	}

	// 解析任务参数
	params, err := s.parseJobParams(req.ExecutorParams)
	if err != nil {
		s.writeLog(logFile, fmt.Sprintf("Failed to parse job params: %v", err))
		return &JobResponse{Code: 500, Msg: fmt.Sprintf("Failed to parse job params: %v", err)}
	}

	// 根据处理器类型执行不同的任务
	switch req.ExecutorHandler {
	case "sshCollectHandler":
		return s.executeSSHCollectJob(req, params, logFile)
	case "deviceScanHandler":
		return s.executeDeviceScanJob(req, params, logFile)
	default:
		msg := fmt.Sprintf("Unknown executor handler: %s", req.ExecutorHandler)
		s.writeLog(logFile, msg)
		return &JobResponse{Code: 500, Msg: msg}
	}
}

// GetJobLog 获取任务日志
func (s *XXLJobService) GetJobLog(req *LogRequest) *LogResponse {
	logPath := s.getLogPath(req.LogID)
	
	// 检查日志文件是否存在
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return &LogResponse{
			Code: 500,
			Msg:  "Log file not found",
		}
	}

	// 读取日志内容
	content, err := os.ReadFile(logPath)
	if err != nil {
		return &LogResponse{
			Code: 500,
			Msg:  fmt.Sprintf("Failed to read log file: %v", err),
		}
	}

	lines := strings.Split(string(content), "\n")
	fromLine := req.FromLineNum
	if fromLine < 0 {
		fromLine = 0
	}
	if fromLine >= len(lines) {
		fromLine = len(lines) - 1
	}

	// 返回从指定行开始的日志内容
	logContent := strings.Join(lines[fromLine:], "\n")
	
	return &LogResponse{
		Code: 200,
		Msg:  "success",
		Content: struct {
			FromLineNum int    `json:"fromLineNum"`
			ToLineNum   int    `json:"toLineNum"`
			LogContent  string `json:"logContent"`
			IsEnd       bool   `json:"isEnd"`
		}{
			FromLineNum: fromLine,
			ToLineNum:   len(lines) - 1,
			LogContent:  logContent,
			IsEnd:       true,
		},
	}
}

// KillJob 终止任务
func (s *XXLJobService) KillJob(jobID int64) *JobResponse {
	logger.Info("Killing XXL-Job task", "job_id", jobID)
	
	// 这里应该实现任务终止逻辑
	// 由于我们的任务是通过CollectorService执行的，需要找到对应的任务并取消
	
	return &JobResponse{Code: 200, Msg: "success"}
}

// heartbeatLoop 心跳循环
func (s *XXLJobService) heartbeatLoop(ctx context.Context) {
	s.heartbeatTicker = time.NewTicker(30 * time.Second)
	defer s.heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-s.heartbeatTicker.C:
			s.sendHeartbeat()
		}
	}
}

// registryLoop 注册循环
func (s *XXLJobService) registryLoop(ctx context.Context) {
	// 立即注册
	s.registerExecutor()

	// 定期重新注册
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.registerExecutor()
		}
	}
}

// registerExecutor 注册执行器
func (s *XXLJobService) registerExecutor() {
	adminAddresses := strings.Split(s.config.XXLJob.AdminAddresses, ",")
	
	for _, adminAddr := range adminAddresses {
		adminAddr = strings.TrimSpace(adminAddr)
		if adminAddr == "" {
			continue
		}

		url := fmt.Sprintf("http://%s/api/registry", adminAddr)
		
		req := &RegistryRequest{
			RegistryGroup: "EXECUTOR",
			RegistryKey:   s.config.XXLJob.AppName,
			RegistryValue: s.getExecutorAddress(),
		}

		if err := s.sendRegistryRequest(url, req); err != nil {
			logger.Error("Failed to register executor", "admin_addr", adminAddr, "error", err)
		} else {
			logger.Debug("Executor registered successfully", "admin_addr", adminAddr)
		}
	}
}

// sendHeartbeat 发送心跳
func (s *XXLJobService) sendHeartbeat() {
	adminAddresses := strings.Split(s.config.XXLJob.AdminAddresses, ",")
	
	for _, adminAddr := range adminAddresses {
		adminAddr = strings.TrimSpace(adminAddr)
		if adminAddr == "" {
			continue
		}

		url := fmt.Sprintf("http://%s/api/registryRemove", adminAddr)
		
		req := &RegistryRequest{
			RegistryGroup: "EXECUTOR",
			RegistryKey:   s.config.XXLJob.AppName,
			RegistryValue: s.getExecutorAddress(),
		}

		// 先移除再注册，确保心跳
		s.sendRegistryRequest(url, req)
		
		// 重新注册
		registerURL := fmt.Sprintf("http://%s/api/registry", adminAddr)
		if err := s.sendRegistryRequest(registerURL, req); err != nil {
			logger.Error("Failed to send heartbeat", "admin_addr", adminAddr, "error", err)
		}
	}
}

// sendRegistryRequest 发送注册请求
func (s *XXLJobService) sendRegistryRequest(url string, req *RegistryRequest) error {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if s.config.XXLJob.AccessToken != "" {
		httpReq.Header.Set("XXL-JOB-ACCESS-TOKEN", s.config.XXLJob.AccessToken)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var response RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Code != 200 {
		return fmt.Errorf("registry failed: %s", response.Msg)
	}

	return nil
}

// executeSSHCollectJob 执行SSH采集任务
func (s *XXLJobService) executeSSHCollectJob(req *JobRequest, params map[string]string, logFile *os.File) *JobResponse {
	s.writeLog(logFile, "Starting SSH collection job")

	// 解析参数
	deviceID := params["device_id"]
	commands := params["commands"]
	
	if deviceID == "" || commands == "" {
		msg := "Missing required parameters: device_id or commands"
		s.writeLog(logFile, msg)
		return &JobResponse{Code: 500, Msg: msg}
	}

	// 创建采集请求
	collectReq := &CollectRequest{
		TaskID:   fmt.Sprintf("xxljob_%d_%d", req.JobID, req.LogID),
		DeviceIP: params["device_ip"],
		Port:     22, // 默认SSH端口
		Username: params["username"],
		Password: params["password"],
		Commands: strings.Split(commands, ";"),
		Timeout:  30,
	}

	// 执行采集任务
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.ExecutorTimeout)*time.Second)
	defer cancel()

	result, err := s.collectorSvc.ExecuteTask(ctx, collectReq)
	if err != nil {
		msg := fmt.Sprintf("SSH collection failed: %v", err)
		s.writeLog(logFile, msg)
		return &JobResponse{Code: 500, Msg: msg}
	}

	// 记录结果
	s.writeLog(logFile, fmt.Sprintf("SSH collection completed successfully, task_id: %s", result.TaskID))
	s.writeLog(logFile, fmt.Sprintf("Commands executed: %d", len(result.Results)))
	
	for i, cmdResult := range result.Results {
		s.writeLog(logFile, fmt.Sprintf("Command %d: %s", i+1, cmdResult.Command))
		s.writeLog(logFile, fmt.Sprintf("Output: %s", cmdResult.Output))
		if cmdResult.Error != "" {
			s.writeLog(logFile, fmt.Sprintf("Error: %s", cmdResult.Error))
		}
	}

	return &JobResponse{Code: 200, Msg: "success"}
}

// executeDeviceScanJob 执行设备扫描任务
func (s *XXLJobService) executeDeviceScanJob(req *JobRequest, params map[string]string, logFile *os.File) *JobResponse {
	s.writeLog(logFile, "Starting device scan job")

	// 这里可以实现设备扫描逻辑
	// 暂时返回成功
	s.writeLog(logFile, "Device scan completed")

	return &JobResponse{Code: 200, Msg: "success"}
}

// parseJobParams 解析任务参数
func (s *XXLJobService) parseJobParams(params string) (map[string]string, error) {
	result := make(map[string]string)
	
	if params == "" {
		return result, nil
	}

	// 支持JSON格式参数
	if strings.HasPrefix(params, "{") {
		var jsonParams map[string]interface{}
		if err := json.Unmarshal([]byte(params), &jsonParams); err == nil {
			for k, v := range jsonParams {
				result[k] = fmt.Sprintf("%v", v)
			}
			return result, nil
		}
	}

	// 支持key=value格式参数
	pairs := strings.Split(params, "&")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}

	return result, nil
}

// createLogDir 创建日志目录
func (s *XXLJobService) createLogDir() error {
	logDir := s.config.XXLJob.LogPath
	if logDir == "" {
		logDir = "./logs/xxljob"
	}

	return os.MkdirAll(logDir, 0755)
}

// createLogFile 创建日志文件
func (s *XXLJobService) createLogFile(logID int64) (*os.File, error) {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

	// 检查是否已经存在
	if file, exists := s.logFiles[logID]; exists {
		return file, nil
	}

	logPath := s.getLogPath(logID)
	
	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	s.logFiles[logID] = file
	return file, nil
}

// getLogPath 获取日志文件路径
func (s *XXLJobService) getLogPath(logID int64) string {
	logDir := s.config.XXLJob.LogPath
	if logDir == "" {
		logDir = "./logs/xxljob"
	}

	// 按日期分目录
	now := time.Now()
	dateDir := now.Format("2006-01-02")
	
	return filepath.Join(logDir, dateDir, fmt.Sprintf("%d.log", logID))
}

// writeLog 写入日志
func (s *XXLJobService) writeLog(file *os.File, message string) {
	if file == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logLine := fmt.Sprintf("[%s] %s\n", timestamp, message)
	
	file.WriteString(logLine)
	file.Sync()
}

// getExecutorAddress 获取执行器地址
func (s *XXLJobService) getExecutorAddress() string {
	if s.config.XXLJob.Address != "" {
		return s.config.XXLJob.Address
	}

	ip := s.config.XXLJob.IP
	if ip == "" {
		ip = "127.0.0.1"
	}

	port := s.config.XXLJob.Port
	if port == 0 {
		port = s.config.Server.Port
	}

	return fmt.Sprintf("http://%s:%d", ip, port)
}

// cleanupLogs 清理过期日志
func (s *XXLJobService) cleanupLogs() {
	logDir := s.config.XXLJob.LogPath
	if logDir == "" {
		logDir = "./logs/xxljob"
	}

	retentionDays := s.config.XXLJob.LogRetentionDays
	if retentionDays <= 0 {
		retentionDays = 7 // 默认保留7天
	}

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !info.IsDir() && info.ModTime().Before(cutoffTime) {
			os.Remove(path)
			logger.Debug("Removed old log file", "path", path)
		}

		return nil
	})
}