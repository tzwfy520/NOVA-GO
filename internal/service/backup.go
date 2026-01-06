package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
	"github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

// ==== 合并自 backup_types.go：请求/响应/模型类型定义 ====

// BackupBatchRequest 批量备份请求
type BackupBatchRequest struct {
	TaskID         string         `json:"task_id"`
	TaskName       string         `json:"task_name,omitempty"`
	TaskBatch      int            `json:"task_batch,omitempty"`
	SaveDir        string         `json:"save_dir,omitempty"`
	StorageBackend string         `json:"storage_backend,omitempty"` // local | minio（默认读取配置）
	RetryFlag      *int           `json:"retry_flag,omitempty"`
	TaskTimeout    *int           `json:"task_timeout,omitempty"`
	Devices        []BackupDevice `json:"devices"`
}

// BackupDevice 备份的设备信息与命令
type BackupDevice struct {
	DeviceIP        string   `json:"device_ip"`
	Port            int      `json:"device_port,omitempty"`
	DeviceName      string   `json:"device_name,omitempty"`
	DevicePlatform  string   `json:"device_platform,omitempty"`
	CollectProtocol string   `json:"collect_protocol,omitempty"` // ssh
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list"`
	DeviceTimeout   *int     `json:"device_timeout,omitempty"`
}

// StoredObject 存储的对象信息
type StoredObject struct {
	URI         string `json:"uri"`
	Size        int64  `json:"size"`
	Checksum    string `json:"checksum"`
	ContentType string `json:"content_type"`
}

// CommandBackupResult 命令备份结果
type CommandBackupResult struct {
	Command       string         `json:"command"`
	RawOutput     string         `json:"raw_output"`
	StoredObjects []StoredObject `json:"stored_objects"`
	ExitCode      int            `json:"exit_code"`
	DurationMS    int64          `json:"duration_ms"`
	Error         string         `json:"error"`
}

// DeviceBackupResponse 设备备份响应
type DeviceBackupResponse struct {
	DeviceIP       string                `json:"device_ip"`
	Port           int                   `json:"port"`
	DeviceName     string                `json:"device_name,omitempty"`
	DevicePlatform string                `json:"device_platform,omitempty"`
	TaskID         string                `json:"task_id"`
	TaskBatch      int                   `json:"task_batch,omitempty"`
	Success        bool                  `json:"success"`
	Results        []CommandBackupResult `json:"results"`
	Error          string                `json:"error"`
	DurationMS     int64                 `json:"duration_ms"`
	Timestamp      time.Time             `json:"timestamp"`
}

// BackupBatchResponse 批量备份响应
type BackupBatchResponse struct {
	Code        string                 `json:"code"`
	Message     string                 `json:"message"`
	Data        []DeviceBackupResponse `json:"data"`
	Total       int                    `json:"total"`
	LogFilePath string                 `json:"log_file_path,omitempty"`
}

// ==== 合并自 storage_writer.go：存储写入器实现 ====

// StorageWriter 抽象存储写入器
type StorageWriter interface {
	Write(ctx context.Context, meta StorageMeta, content string, contentType string) (StoredObject, error)
}

// StorageMeta 写入元数据
type StorageMeta struct {
	SaveDir      string
	DateYYYYMMDD string
	// TimeHHMMSS 设备任务开始时间（统一目录时间戳），格式为 HHMMSS
	TimeHHMMSS     string
	TaskID         string
	DeviceName     string
	DeviceIP       string
	DevicePlatform string
	CommandSlug    string
	Backend        string // local|minio
}

// NewStorageWriter 根据配置创建写入器（委派到本地或 MinIO）
func NewStorageWriter(cfg *config.Config) StorageWriter {
	// 委派写入器：根据 meta.Backend 路由
	dw := &DelegatingStorageWriter{cfg: cfg, local: &LocalStorageWriter{cfg: cfg}}
	// 初始化 MinIO 写入器（统一文件实现）
	dw.minio = initMinioWriter(cfg)
	return dw
}

// DelegatingStorageWriter 按后端路由写入
type DelegatingStorageWriter struct {
	cfg   *config.Config
	local *LocalStorageWriter
	minio *MinioStorageWriter
}

func (w *DelegatingStorageWriter) Write(ctx context.Context, meta StorageMeta, content string, contentType string) (StoredObject, error) {
	backend := strings.ToLower(strings.TrimSpace(meta.Backend))
	if backend == "minio" {
		if w.minio == nil {
			// MinIO 未初始化：记录预警并回退到本地
			logger.Warn("MinIO backend selected but client not initialized; falling back to local")
			obj, lerr := w.local.Write(ctx, meta, content, contentType)
			if lerr != nil {
				return StoredObject{}, fmt.Errorf("minio client not initialized; local fallback failed: %w", lerr)
			}
			// 返回对象同时返回预警错误，便于上层记录但不中断流程
			return obj, fmt.Errorf("minio client not initialized; wrote to local instead")
		}
		// 先尝试 MinIO 写入
		obj, err := w.minio.Write(ctx, meta, content, contentType)
		if err != nil {
			// 失败则记录预警并回退到本地
			logger.Warn("MinIO write failed; falling back to local", "error", err)
			objLocal, lerr := w.local.Write(ctx, meta, content, contentType)
			if lerr != nil {
				return StoredObject{}, fmt.Errorf("minio write failed: %v; local fallback failed: %w", err, lerr)
			}
			// 返回本地对象，并携带预警错误说明
			return objLocal, fmt.Errorf("minio write failed: %w; fell back to local successfully", err)
		}
		return obj, nil
	}
	// 默认走本地
	return w.local.Write(ctx, meta, content, contentType)
}

// LocalStorageWriter 本地文件写入
type LocalStorageWriter struct {
	cfg *config.Config
}

func (w *LocalStorageWriter) Write(ctx context.Context, meta StorageMeta, content string, contentType string) (StoredObject, error) {
	baseDir := strings.TrimSpace(w.cfg.Backup.Local.BaseDir)
	if baseDir == "" {
		baseDir = "./data/backups"
	}

	// 层级：baseDir / backup.prefix / local.prefix / save_dir / device / taskID
	parts := []string{baseDir}
	if p := strings.TrimSpace(w.cfg.Backup.Prefix); p != "" {
		parts = append(parts, p)
	}
	if p := strings.TrimSpace(w.cfg.Backup.Local.Prefix); p != "" {
		parts = append(parts, p)
	}
	if sd := strings.TrimSpace(meta.SaveDir); sd != "" {
		parts = append(parts, sd)
	}

	deviceLabel := strings.TrimSpace(meta.DeviceName)
	if deviceLabel == "" {
		deviceLabel = strings.TrimSpace(meta.DeviceIP)
	}
	deviceLabel = slug(deviceLabel)

	parts = append(parts, deviceLabel)
	if tid := strings.TrimSpace(meta.TaskID); tid != "" {
		parts = append(parts, tid)
	}

	dirPath := filepath.Join(parts...)

	if w.cfg.Backup.Local.MkdirIfMissing {
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			return StoredObject{}, fmt.Errorf("failed to create dir: %w", err)
		}
	}

	// 过滤输出（按平台配置优先，回退到全局配置）
	filtered := applyPlatformLineFilter(w.cfg, meta.DevicePlatform, content)

	// 文件名：命令 slug 或显式文件名（目录已带时分秒避免覆盖）
	// 若传入已包含扩展名，则不再追加 .txt
	base := slug(meta.CommandSlug)
	filename := base
	if !strings.Contains(base, ".") {
		filename = base + ".txt"
	}
	fullPath := filepath.Join(dirPath, filename)

	// 写入文件
	data := []byte(filtered)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return StoredObject{}, fmt.Errorf("failed to write file: %w", err)
	}

	// 计算校验
	sum := sha256.Sum256(data)
	chk := "sha256:" + hex.EncodeToString(sum[:])

	// 返回对象信息
	uri := "file://" + fullPath
	return StoredObject{
		URI:      uri,
		Size:     int64(len(data)),
		Checksum: chk,
		ContentType: func() string {
			if contentType != "" {
				return contentType
			}
			return "text/plain; charset=utf-8"
		}(),
	}, nil
}

// MinioStorageWriter MinIO 对象存储写入（统一文件实现）
type MinioStorageWriter struct {
	cfg           *config.Config
	client        *minio.Client
	endpoint      string
	bucketEnsured bool
}

// initMinioWriter 尝试初始化 MinIO 写入器（包含合理的超时设置与连通性校验）
func initMinioWriter(cfg *config.Config) *MinioStorageWriter {
	host := strings.TrimSpace(cfg.Storage.Minio.Host)
	port := cfg.Storage.Minio.Port
	if host == "" || port <= 0 {
		logger.Warn("MinIO configuration incomplete; host/port missing")
		return nil
	}
	endpoint := fmt.Sprintf("%s:%d", host, port)

	// 自定义传输以提升连接与响应的鲁棒性
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.Storage.Minio.AccessKey, cfg.Storage.Minio.SecretKey, ""),
		Secure:    cfg.Storage.Minio.Secure,
		Transport: transport,
	})
	if err != nil {
		logger.Error("MinIO client initialization failed", "error", err)
		return nil
	}

	w := &MinioStorageWriter{cfg: cfg, client: client, endpoint: endpoint}

	// 进行一次轻量连通性与 bucket 校验（不影响整体初始化）
	bucket := strings.TrimSpace(cfg.Storage.Minio.Bucket)
	if bucket == "" {
		logger.Warn("MinIO bucket not configured")
		return w
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.ensureBucket(ctx, bucket, 2); err != nil {
		logger.Warn("MinIO bucket ensure at init failed", "error", err)
	} else {
		w.bucketEnsured = true
	}
	return w
}

// Write 将内容写入 MinIO
func (w *MinioStorageWriter) Write(ctx context.Context, meta StorageMeta, content string, contentType string) (StoredObject, error) {
	if w == nil || w.client == nil {
		return StoredObject{}, fmt.Errorf("minio client not initialized")
	}
	bucket := strings.TrimSpace(w.cfg.Storage.Minio.Bucket)
	if bucket == "" {
		return StoredObject{}, fmt.Errorf("minio bucket not configured")
	}

	// 过滤输出（按平台配置优先，回退到全局配置）
	filtered := applyPlatformLineFilter(w.cfg, meta.DevicePlatform, content)

	// 构造对象路径（使用 POSIX 风格，与本地一致）
	parts := []string{}
	if p := strings.TrimSpace(w.cfg.Backup.Prefix); p != "" {
		parts = append(parts, p)
	}
	if p := strings.TrimSpace(w.cfg.Backup.Local.Prefix); p != "" {
		parts = append(parts, p)
	}
	if sd := strings.TrimSpace(meta.SaveDir); sd != "" {
		parts = append(parts, sd)
	}
	deviceLabel := strings.TrimSpace(meta.DeviceName)
	if deviceLabel == "" {
		deviceLabel = strings.TrimSpace(meta.DeviceIP)
	}
	deviceLabel = slug(deviceLabel)
	parts = append(parts, deviceLabel)
	if tid := strings.TrimSpace(meta.TaskID); tid != "" {
		parts = append(parts, tid)
	}

	// 文件名：命令 slug 或显式文件名（与本地规则一致）
	base := slug(meta.CommandSlug)
	filename := base
	if !strings.Contains(base, ".") {
		filename = base + ".txt"
	}
	objectName := path.Join(strings.Join(parts, "/"), filename)

	data := []byte(filtered)
	ct := contentType
	if ct == "" {
		ct = "text/plain; charset=utf-8"
	}

	// 写入前快速连通性探测（失败则尽早返回明确错误）
	if err := w.fastConnectivityCheck(ctx); err != nil {
		return StoredObject{}, fmt.Errorf("minio connectivity failed to %s: %w", w.endpoint, err)
	}

	// 需要时确保 bucket（有限重试）
	if !w.bucketEnsured {
		if err := w.ensureBucket(ctx, bucket, 3); err != nil {
			return StoredObject{}, fmt.Errorf("minio ensure bucket failed: %w", err)
		}
		w.bucketEnsured = true
	}

	// 带重试的对象写入（指数退避），使用请求上下文剩余时间做上限
	var lastErr error
	attempts := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i := 0; i < len(attempts); i++ {
		r := bytes.NewReader(data)
		attemptCtx, cancel := w.attemptContext(ctx, attempts[i])
		_, err := w.client.PutObject(attemptCtx, bucket, objectName, r, int64(len(data)), minio.PutObjectOptions{ContentType: ct})
		cancel()
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		time.Sleep(attempts[i])
	}
	if lastErr != nil {
		return StoredObject{}, fmt.Errorf("minio put object failed after retries: %w", lastErr)
	}

	// 计算校验
	sum := sha256.Sum256(data)
	chk := "sha256:" + hex.EncodeToString(sum[:])

	// 返回对象信息
	uri := "minio://" + path.Join(bucket, objectName)
	return StoredObject{
		URI:         uri,
		Size:        int64(len(data)),
		Checksum:    chk,
		ContentType: ct,
	}, nil
}

// fastConnectivityCheck 使用 TCP 直连做快速连通性校验
func (w *MinioStorageWriter) fastConnectivityCheck(parent context.Context) error {
	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(parent, "tcp", w.endpoint)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func ensureMinioBucket(parent context.Context, client *minio.Client, bucket string, retries int, attemptContext func(context.Context, time.Duration) (context.Context, context.CancelFunc)) error {
	var lastErr error
	for i := 0; i <= retries; i++ {
		ctx, cancel := attemptContext(parent, 10*time.Second)
		exists, err := client.BucketExists(ctx, bucket)
		cancel()
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		if exists {
			return nil
		}
		ctx2, cancel2 := attemptContext(parent, 10*time.Second)
		if mkErr := client.MakeBucket(ctx2, bucket, minio.MakeBucketOptions{}); mkErr != nil {
			lastErr = mkErr
			cancel2()
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		cancel2()
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("bucket ensure failed for %s", bucket)
}

// ensureBucket 校验并创建 bucket，支持有限重试
func (w *MinioStorageWriter) ensureBucket(parent context.Context, bucket string, retries int) error {
	return ensureMinioBucket(parent, w.client, bucket, retries, w.attemptContext)
}

// attemptContext 构造限时上下文，尊重父上下文的剩余截止时间
func (w *MinioStorageWriter) attemptContext(parent context.Context, prefer time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok {
		remain := time.Until(deadline)
		if remain > time.Second && prefer < remain {
			return context.WithTimeout(parent, prefer)
		}
		if remain > time.Second {
			return context.WithTimeout(parent, remain-time.Second)
		}
		return context.WithTimeout(parent, time.Second)
	}
	return context.WithTimeout(parent, prefer)
}

// applyLineFilter 按前缀/包含过滤行
func applyLineFilter(f config.OutputFilterConfig, s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		raw := ln
		cmp := ln
		if f.TrimSpace {
			cmp = strings.TrimSpace(cmp)
		}
		if f.CaseInsensitive {
			cmp = strings.ToLower(cmp)
		}
		// 前缀匹配
		matched := false
		for _, p := range f.Prefixes {
			pv := p
			if f.TrimSpace {
				pv = strings.TrimSpace(pv)
			}
			if f.CaseInsensitive {
				pv = strings.ToLower(pv)
			}
			if pv == "" {
				continue
			}
			if strings.HasPrefix(cmp, pv) {
				matched = true
				break
			}
		}
		if !matched {
			for _, c := range f.Contains {
				cv := c
				if f.TrimSpace {
					cv = strings.TrimSpace(cv)
				}
				if f.CaseInsensitive {
					cv = strings.ToLower(cv)
				}
				if cv == "" {
					continue
				}
				if strings.Contains(cmp, cv) {
					matched = true
					break
				}
			}
		}
		if !matched {
			out = append(out, raw)
		}
	}
	return strings.Join(out, "\n")
}

// getOutputFilterForPlatform 返回平台对应的输出过滤配置；若平台未配置则回退 default 平台
func getOutputFilterForPlatform(cfg *config.Config, platform string) config.OutputFilterConfig {
	if cfg == nil {
		return config.OutputFilterConfig{}
	}
	if dd, _, ok := cfg.GetDeviceDefaults(platform); ok {
		if len(dd.OutputFilter.Prefixes) > 0 || len(dd.OutputFilter.Contains) > 0 {
			return dd.OutputFilter
		}
	}
	return cfg.Collector.OutputFilter
}

// applyPlatformLineFilter 根据设备平台选择过滤规则并应用
func applyPlatformLineFilter(cfg *config.Config, platform string, s string) string {
	return applyLineFilter(getOutputFilterForPlatform(cfg, platform), s)
}

var slugRe = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = slugRe.ReplaceAllString(s, "")
	if s == "" {
		s = "unknown"
	}
	return s
}

func previewRawOutput(raw string, limit int) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	pv := strings.Join(lines, "\n")
	pv = strings.TrimRight(pv, "\n")
	if strings.TrimSpace(pv) == "" {
		return ""
	}
	return "pre-view: " + pv
}

// BackupService 配置备份服务。
// 交互说明：设备命令执行统一走 InteractBasic（交互优先、失败回退非交互逻辑已内联到 InteractBasic），包含平台预命令注入与结果过滤。
// 职责边界：本服务仅做任务编排与存储写入；不参与预命令注入或输出过滤。
type BackupService struct {
	config        *config.Config
	sshPool       *ssh.Pool
	running       bool
	workers       chan struct{}
	interact      *InteractBasic
	storageWriter StorageWriter
}

// NewBackupService 创建备份服务
func NewBackupService(cfg *config.Config) *BackupService {
	conc := cfg.Collector.Concurrent
	if conc <= 0 {
		conc = 1
	}
	threads := cfg.Collector.Threads
	if threads <= 0 {
		threads = cfg.SSH.MaxSessions
	}
	poolConfig := &ssh.PoolConfig{
		MaxIdle:         10,
		MaxActive:       conc,
		IdleTimeout:     5 * time.Minute,
		CleanupInterval: cfg.SSH.CleanupInterval,
		SSHConfig: &ssh.Config{
			Timeout:        cfg.SSH.Timeout,
			ConnectTimeout: cfg.SSH.ConnectTimeout,
			KeepAlive:      cfg.SSH.KeepAliveInterval,
			MaxSessions:    threads,
		},
	}

	pool := ssh.NewPool(poolConfig)
	return &BackupService{
		config:        cfg,
		sshPool:       pool,
		workers:       make(chan struct{}, conc),
		interact:      NewInteractBasic(cfg, pool),
		storageWriter: NewStorageWriter(cfg),
	}
}

// Start 启动服务
func (s *BackupService) Start(ctx context.Context) error {
	if s.running {
		return fmt.Errorf("backup service is already running")
	}
	s.running = true
	logger.Info("Backup service started")
	return nil
}

// Stop 停止服务
func (s *BackupService) Stop() error {
	if !s.running {
		return nil
	}
	s.running = false
	if err := s.sshPool.Close(); err != nil {
		logger.Error("Failed to close SSH pool (backup)", "error", err)
	}
	logger.Info("Backup service stopped")
	return nil
}

// ExecuteBatch 执行批量备份
func (s *BackupService) ExecuteBatch(ctx context.Context, req *BackupBatchRequest) (*BackupBatchResponse, error) {
	if !s.running {
		return nil, fmt.Errorf("backup service is not running")
	}
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	// 防傻机制：task_id 为空时使用当前时间戳（yyyyMMddHHmm）作为 task_id
	if strings.TrimSpace(req.TaskID) == "" {
		req.TaskID = time.Now().Format("200601021504")
		logger.Warn("empty task_id; using timestamp fallback", "task_id", req.TaskID)
	}
	if len(req.Devices) == 0 {
		return nil, fmt.Errorf("devices is empty")
	}

	// 创建按任务日志文件：logs/backup/<task_id>_<YYYYMMDD_HHMMSS>.log
	logDir := filepath.Join("logs", "backup")
	_ = os.MkdirAll(logDir, 0o755)
	logName := fmt.Sprintf("%s_%s.log", strings.TrimSpace(req.TaskID), time.Now().Format("20060102_150405"))
	logFilePath := filepath.Join(logDir, logName)
	var writeMu sync.Mutex

	// 并发执行各设备备份
	type item struct {
		resp DeviceBackupResponse
	}
	out := make([]item, len(req.Devices))
	var wg sync.WaitGroup
	wg.Add(len(req.Devices))

	for i := range req.Devices {
		idx := i
		dev := req.Devices[i]

		// 队列限流：等待工作令牌，避免 HTTP ctx 过早结束
		go func() {
			// 采用有效超时作为队列等待窗口
			effTimeout := s.effectiveTimeout(req.TaskTimeout, dev.DevicePlatform)
			waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Duration(effTimeout)*time.Second)
			defer waitCancel()
			select {
			case s.workers <- struct{}{}:
				defer func() { <-s.workers }()
			case <-waitCtx.Done():
				out[idx].resp = DeviceBackupResponse{
					DeviceIP: dev.DeviceIP,
					Port: func() int {
						if dev.Port < 1 || dev.Port > 65535 {
							return 22
						}
						return dev.Port
					}(),
					DeviceName:     dev.DeviceName,
					DevicePlatform: dev.DevicePlatform,
					TaskID:         req.TaskID,
					TaskBatch:      req.TaskBatch,
					Success:        false,
					Error:          fmt.Sprintf("queue wait timeout after %ds", effTimeout),
					DurationMS:     0,
					Timestamp:      time.Now(),
				}
				wg.Done()
				return
			}

			start := time.Now()
			resp := DeviceBackupResponse{
				DeviceIP: dev.DeviceIP,
				Port: func() int {
					if dev.Port < 1 || dev.Port > 65535 {
						return 22
					}
					return dev.Port
				}(),
				DeviceName:     dev.DeviceName,
				DevicePlatform: dev.DevicePlatform,
				TaskID:         req.TaskID,
				TaskBatch:      req.TaskBatch,
				Timestamp:      start,
			}

			// 执行命令
			execReq := &ExecRequest{
				DeviceIP:        dev.DeviceIP,
				Port:            dev.Port,
				DeviceName:      dev.DeviceName,
				DevicePlatform:  dev.DevicePlatform,
				CollectProtocol: dev.CollectProtocol,
				UserName:        dev.UserName,
				Password:        dev.Password,
				EnablePassword:  dev.EnablePassword,
				TaskTimeoutSec:  s.effectiveTimeout(req.TaskTimeout, dev.DevicePlatform),
				DeviceTimeoutSec: func() int {
					if dev.DeviceTimeout != nil && *dev.DeviceTimeout > 0 {
						return *dev.DeviceTimeout
					}
					return s.effectiveTimeout(req.TaskTimeout, dev.DevicePlatform)
				}(),
				TaskID:  req.TaskID,
				LogType: "backup",
			}

			// 支持有限重试（请求优先，平台默认回退）
			var results []*ssh.CommandResult
			var err error
			retries := s.effectiveRetries(req.RetryFlag, dev.DevicePlatform)
			for attempt := 0; attempt <= retries; attempt++ {
				results, err = s.interact.Execute(ctx, execReq, dev.CliList)
				if err == nil {
					break
				}
				if attempt < retries {
					time.Sleep(300 * time.Millisecond)
				}
			}
			if err != nil {
				resp.Success = false
				resp.Error = err.Error()
				resp.DurationMS = time.Since(start).Milliseconds()
				// 记录登录失败到按任务日志文件
				writeMu.Lock()
				func() {
					f, e := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
					if e == nil {
						defer f.Close()
						dlabel := strings.TrimSpace(dev.DeviceName)
						if dlabel == "" {
							dlabel = strings.TrimSpace(dev.DeviceIP)
						}
						line := map[string]interface{}{
							"time":        time.Now().Format("2006-01-02 15:04:05"),
							"level":       "error",
							"log_type":    "backup",
							"task_id":     strings.TrimSpace(req.TaskID),
							"device":      dlabel,
							"command":     "__login__",
							"status":      "失败",
							"exit_code":   -1,
							"duration_ms": resp.DurationMS,
							"msg":         fmt.Sprintf("task_trace: device %s 登录失败", dlabel),
						}
						if data, e2 := json.Marshal(line); e2 == nil {
							_, _ = f.Write(append(data, '\n'))
						}
					}
				}()
				writeMu.Unlock()
				out[idx].resp = resp
				wg.Done()
				return
			}

			// 写入存储并组装响应
			date := time.Now().Format("20060102")
			backend := strings.TrimSpace(req.StorageBackend)
			if backend == "" {
				backend = strings.TrimSpace(s.config.Backup.StorageBackend)
			}
			if backend == "" {
				backend = "local"
			}

			resp.Results = make([]CommandBackupResult, 0, len(results))
			for _, r := range results {
				// 预处理命令不落盘，仅记录输出（例如 enable、关闭分页等）
				isPre := s.isPreCommand(dev.DevicePlatform, r.Command)

				stored := []StoredObject{}
				storeErrMsg := ""
				// 当 aggregate_only 启用时，跳过逐命令写入，仅生成聚合文件
				if !isPre && !s.config.Backup.Aggregate.AggregateOnly {
					// 仅对采集命令进行存储
					meta := StorageMeta{
						SaveDir:        req.SaveDir,
						DateYYYYMMDD:   date,
						TimeHHMMSS:     start.Format("150405"),
						TaskID:         req.TaskID,
						DeviceName:     dev.DeviceName,
						DeviceIP:       dev.DeviceIP,
						DevicePlatform: dev.DevicePlatform,
						CommandSlug:    r.Command,
						Backend:        backend,
					}
					obj, werr := s.storageWriter.Write(ctx, meta, r.Output, "text/plain; charset=utf-8")
					if obj.URI != "" {
						stored = append(stored, obj)
					}
					if werr != nil {
						storeErrMsg = werr.Error()
					}
				}

				// 记录命令执行 task_trace 到按任务日志文件
				writeMu.Lock()
				func() {
					f, e := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
					if e == nil {
						defer f.Close()
						dlabel := strings.TrimSpace(dev.DeviceName)
						if dlabel == "" {
							dlabel = strings.TrimSpace(dev.DeviceIP)
						}
						execOK := r.ExitCode == 0 && strings.TrimSpace(r.Error) == "" && storeErrMsg == ""
						status := "失败"
						if execOK {
							status = "成功"
						}
						line := map[string]interface{}{
							"time":        time.Now().Format("2006-01-02 15:04:05"),
							"level":       "info",
							"log_type":    "backup",
							"task_id":     strings.TrimSpace(req.TaskID),
							"device":      dlabel,
							"command":     strings.TrimSpace(r.Command),
							"status":      status,
							"exit_code":   r.ExitCode,
							"duration_ms": r.Duration.Milliseconds(),
							"msg":         fmt.Sprintf("task_trace: device %s 执行 %s", dlabel, strings.TrimSpace(r.Command)),
						}
						if data, e2 := json.Marshal(line); e2 == nil {
							_, _ = f.Write(append(data, '\n'))
						}
					}
				}()
				writeMu.Unlock()

				resp.Results = append(resp.Results, CommandBackupResult{
					Command:       r.Command,
					RawOutput:     previewRawOutput(r.Output, 5),
					StoredObjects: stored,
					ExitCode:      r.ExitCode,
					DurationMS:    r.Duration.Milliseconds(),
					Error: func() string {
						if r.Error != "" {
							return r.Error
						}
						return storeErrMsg
					}(),
				})
			}

			// 聚合写入：受配置控制，将所有采集命令输出汇总到单一文件（不包含预处理命令）
			// 当 aggregate_only=true 时，即便未显式开启 enabled，也生成聚合文件
			if s.config.Backup.Aggregate.Enabled || s.config.Backup.Aggregate.AggregateOnly {
				var aggBuilder strings.Builder
				// 统一的设备与时间，用于段落标识
				devName := strings.TrimSpace(dev.DeviceName)
				if devName == "" {
					devName = dev.DeviceIP
				}
				ts := start.Format("2006-01-02 15:04:05")
				for _, r := range results {
					if s.isPreCommand(dev.DevicePlatform, r.Command) {
						continue
					}
					cmdTitle := strings.TrimSpace(r.Command)
					if cmdTitle == "" {
						cmdTitle = "unknown"
					}
					// 段落头：=== <cmd> ===，下一行附设备名与时间
					aggBuilder.WriteString("=== ")
					aggBuilder.WriteString(cmdTitle)
					aggBuilder.WriteString(" ===\n")
					aggBuilder.WriteString("Device: ")
					aggBuilder.WriteString(devName)
					aggBuilder.WriteString(" | Time: ")
					aggBuilder.WriteString(ts)
					aggBuilder.WriteString("\n")
					if r.Output != "" {
						aggBuilder.WriteString(r.Output)
						if !strings.HasSuffix(r.Output, "\n") {
							aggBuilder.WriteString("\n")
						}
					}
					aggBuilder.WriteString("\n")
				}
				aggContent := aggBuilder.String()
				if strings.TrimSpace(aggContent) != "" {
					// 聚合文件名可配置，允许带扩展名
					aggName := strings.TrimSpace(s.config.Backup.Aggregate.Filename)
					if aggName == "" {
						aggName = "all_cli.txt"
					}
					metaAll := StorageMeta{
						SaveDir:        req.SaveDir,
						DateYYYYMMDD:   date,
						TimeHHMMSS:     start.Format("150405"),
						TaskID:         req.TaskID,
						DeviceName:     dev.DeviceName,
						DeviceIP:       dev.DeviceIP,
						DevicePlatform: dev.DevicePlatform,
						CommandSlug:    aggName,
						Backend:        backend,
					}
					obj, werr := s.storageWriter.Write(ctx, metaAll, aggContent, "text/plain; charset=utf-8")
					storedList := []StoredObject{}
					if obj.URI != "" {
						storedList = []StoredObject{obj}
					}
					errMsg := ""
					if werr != nil {
						errMsg = werr.Error()
					}
					resp.Results = append(resp.Results, CommandBackupResult{
						Command:       aggName,
						RawOutput:     previewRawOutput(aggContent, 5),
						StoredObjects: storedList,
						ExitCode:      0,
						DurationMS:    0,
						Error:         errMsg,
					})
				}
			}

			// 成功条件：至少有结果且不含致命错误
			resp.Success = len(resp.Results) > 0 && resp.Error == ""
			resp.DurationMS = time.Since(start).Milliseconds()
			out[idx].resp = resp
			wg.Done()
		}()
	}

	wg.Wait()

	// 汇总响应
	final := &BackupBatchResponse{
		Code:        "SUCCESS",
		Message:     "batch backup executed",
		Data:        make([]DeviceBackupResponse, 0, len(out)),
		Total:       len(out),
		LogFilePath: logFilePath,
	}
	anyFail := false
	for _, it := range out {
		final.Data = append(final.Data, it.resp)
		if !it.resp.Success {
			anyFail = true
		}
	}
	if anyFail {
		final.Code = "PARTIAL_SUCCESS"
		final.Message = "some devices failed"
	}
	return final, nil
}

func (s *BackupService) effectiveTimeout(reqTimeout *int, platform string) int {
	if reqTimeout != nil && *reqTimeout > 0 {
		return *reqTimeout
	}
	d := getPlatformDefaults(strings.ToLower(strings.TrimSpace(func() string {
		if platform == "" {
			return "default"
		}
		return platform
	}())))
	if d.Timeout > 0 {
		return d.Timeout
	}
	return 30
}

// effectiveRetries 计算有效重试次数：请求参数优先，其次平台默认，最后回退到 collector.retry_flags
func (s *BackupService) effectiveRetries(reqRetries *int, platform string) int {
	if reqRetries != nil && *reqRetries >= 0 {
		return *reqRetries
	}
	d := getPlatformDefaults(strings.ToLower(strings.TrimSpace(func() string {
		if platform == "" {
			return "default"
		}
		return platform
	}())))
	if d.Retries > 0 {
		return d.Retries
	}
	if s.config != nil && s.config.Collector.RetryFlags > 0 {
		return s.config.Collector.RetryFlags
	}
	return 0
}

// isPreCommand 判断是否为平台级预处理命令（如 enable、关闭分页），这些命令不参与落盘
func (s *BackupService) isPreCommand(platform, cmd string) bool {
	c := strings.ToLower(strings.TrimSpace(cmd))
	if c == "" {
		return false
	}
	p := strings.ToLower(strings.TrimSpace(platform))

	if s != nil && s.config != nil {
		dd, _, ok := s.config.GetDeviceDefaults(p)
		// 提权命令
		ecmd := strings.TrimSpace(dd.EnableCLI)
		if ecmd == "" && dd.EnableRequired {
			ecmd = "enable"
		}
		if ecmd != "" && strings.ToLower(ecmd) == c {
			return true
		}
		// 关闭分页命令
		for _, pc := range dd.DisablePagingCmds {
			if strings.ToLower(strings.TrimSpace(pc)) == c {
				return true
			}
		}
		_ = ok
	}
	// 通用兜底
	if c == "enable" || c == "terminal length 0" || c == "screen-length disable" {
		return true
	}
	return false
}
