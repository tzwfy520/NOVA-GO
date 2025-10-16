package service

import (
    "bytes"
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net"
    "net/http"
    "os"
    "path"
    "path/filepath"
    "regexp"
    "strings"
    "time"

    minio "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
    "github.com/sshcollectorpro/sshcollectorpro/internal/config"
    "github.com/sshcollectorpro/sshcollectorpro/pkg/logger"
)

// StorageWriter 抽象存储写入器
type StorageWriter interface {
    Write(ctx context.Context, meta StorageMeta, content string, contentType string) (StoredObject, error)
}

// StorageMeta 写入元数据
type StorageMeta struct {
    SaveDir     string
    DateYYYYMMDD string
    // TimeHHMMSS 设备任务开始时间（统一目录时间戳），格式为 HHMMSS
    TimeHHMMSS string
    TaskID      string
    DeviceName  string
    DeviceIP    string
    CommandSlug string
    Backend     string // local|minio
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
    if baseDir == "" { baseDir = "./data/backups" }

    // 层级：baseDir / backup.prefix / local.prefix / save_dir / device / date / taskID
    parts := []string{baseDir}
    if p := strings.TrimSpace(w.cfg.Backup.Prefix); p != "" { parts = append(parts, p) }
    if p := strings.TrimSpace(w.cfg.Backup.Local.Prefix); p != "" { parts = append(parts, p) }
    if sd := strings.TrimSpace(meta.SaveDir); sd != "" { parts = append(parts, sd) }

    deviceLabel := strings.TrimSpace(meta.DeviceName)
    if deviceLabel == "" { deviceLabel = strings.TrimSpace(meta.DeviceIP) }
    deviceLabel = slug(deviceLabel)

    parts = append(parts, deviceLabel)
    // 目录层增加统一的设备任务开始时间，例如 20251016_145830
    datePart := strings.TrimSpace(meta.DateYYYYMMDD)
    if datePart == "" { datePart = time.Now().Format("20060102") }
    timePart := strings.TrimSpace(meta.TimeHHMMSS)
    if timePart == "" { timePart = time.Now().Format("150405") }
    parts = append(parts, fmt.Sprintf("%s_%s", datePart, timePart))
    if tid := strings.TrimSpace(meta.TaskID); tid != "" { parts = append(parts, tid) }

    dirPath := filepath.Join(parts...)

    if w.cfg.Backup.Local.MkdirIfMissing {
        if err := os.MkdirAll(dirPath, 0o755); err != nil {
            return StoredObject{}, fmt.Errorf("failed to create dir: %w", err)
        }
    }

    // 过滤输出（按配置）
    filtered := applyLineFilter(w.cfg.Collector.OutputFilter, content)

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
        URI:         uri,
        Size:        int64(len(data)),
        Checksum:    chk,
        ContentType: func() string { if contentType != "" { return contentType } ; return "text/plain; charset=utf-8" }(),
    }, nil
}

// MinioStorageWriter MinIO 对象存储写入（统一文件实现）
type MinioStorageWriter struct {
    cfg    *config.Config
    client *minio.Client
    endpoint string
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
        DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
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

    // 过滤输出（按配置）
    filtered := applyLineFilter(w.cfg.Collector.OutputFilter, content)

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
    datePart := strings.TrimSpace(meta.DateYYYYMMDD)
    if datePart == "" { datePart = time.Now().Format("20060102") }
    timePart := strings.TrimSpace(meta.TimeHHMMSS)
    if timePart == "" { timePart = time.Now().Format("150405") }
    parts = append(parts, fmt.Sprintf("%s_%s", datePart, timePart))
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
        if err == nil { lastErr = nil; break }
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
    if err != nil { return err }
    _ = conn.Close()
    return nil
}

// ensureBucket 校验并创建 bucket，支持有限重试
func (w *MinioStorageWriter) ensureBucket(parent context.Context, bucket string, retries int) error {
    var lastErr error
    for i := 0; i <= retries; i++ {
        ctx, cancel := w.attemptContext(parent, 10*time.Second)
        exists, err := w.client.BucketExists(ctx, bucket)
        cancel()
        if err != nil {
            lastErr = err
            time.Sleep(time.Duration(i+1) * time.Second)
            continue
        }
        if exists { return nil }
        ctx2, cancel2 := w.attemptContext(parent, 10*time.Second)
        if mkErr := w.client.MakeBucket(ctx2, bucket, minio.MakeBucketOptions{}); mkErr != nil {
            lastErr = mkErr
            cancel2()
            time.Sleep(time.Duration(i+1) * time.Second)
            continue
        }
        cancel2()
        return nil
    }
    if lastErr != nil { return lastErr }
    return fmt.Errorf("bucket ensure failed for %s", bucket)
}

// attemptContext 构造限时上下文，尊重父上下文的剩余截止时间
func (w *MinioStorageWriter) attemptContext(parent context.Context, prefer time.Duration) (context.Context, context.CancelFunc) {
    if deadline, ok := parent.Deadline(); ok {
        remain := time.Until(deadline)
        if remain > time.Second && prefer < remain {
            return context.WithTimeout(parent, prefer)
        }
        if remain > time.Second {
            return context.WithTimeout(parent, remain - time.Second)
        }
        return context.WithTimeout(parent, time.Second)
    }
    return context.WithTimeout(parent, prefer)
}

// applyLineFilter 按前缀/包含过滤行
func applyLineFilter(f config.OutputFilterConfig, s string) string {
    if s == "" { return s }
    lines := strings.Split(s, "\n")
    out := make([]string, 0, len(lines))
    for _, ln := range lines {
        raw := ln
        cmp := ln
        if f.TrimSpace { cmp = strings.TrimSpace(cmp) }
        if f.CaseInsensitive { cmp = strings.ToLower(cmp) }
        // 前缀匹配
        matched := false
        for _, p := range f.Prefixes {
            pv := p
            if f.CaseInsensitive { pv = strings.ToLower(pv) }
            if f.TrimSpace { cmp = strings.TrimSpace(cmp) }
            if strings.HasPrefix(cmp, pv) { matched = true; break }
        }
        if !matched {
            for _, c := range f.Contains {
                cv := c
                if f.CaseInsensitive { cv = strings.ToLower(cv) }
                if strings.Contains(cmp, cv) { matched = true; break }
            }
        }
        if !matched {
            out = append(out, raw)
        }
    }
    return strings.Join(out, "\n")
}

var slugRe = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
    s = strings.TrimSpace(s)
    s = strings.ToLower(s)
    s = strings.ReplaceAll(s, " ", "_")
    s = strings.ReplaceAll(s, "/", "_")
    s = strings.ReplaceAll(s, "\\", "_")
    s = slugRe.ReplaceAllString(s, "")
    if s == "" { s = "unknown" }
    return s
}