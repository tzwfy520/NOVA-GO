package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path"
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

// ====== 请求/响应类型定义 ======

type FormatBatchRequest struct {
	TaskID       string           `json:"task_id"`
	TaskName     string           `json:"task_name,omitempty"`
	TaskBatch    int              `json:"task_batch,omitempty"`
	RetryFlag    *int             `json:"retry_flag,omitempty"`
	SaveDir      string           `json:"save_dir"`
	Timeout      *int             `json:"timeout,omitempty"`
	FSMTemplates []FSMTemplateDef `json:"fsm_templates"`
	Devices      []FormatDevice   `json:"devices"`
}

type FormatDevice struct {
	DeviceIP        string   `json:"device_ip"`
	DevicePort      int      `json:"device_port,omitempty"`
	DeviceName      string   `json:"device_name"`
	DevicePlatform  string   `json:"device_platform"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list"`
}

// FSM 模板定义：按平台与命令组织
type FSMTemplateValue struct {
	CLIName  string `json:"cli_name"`
	FSMValue string `json:"fsm_value"`
}
type FSMTemplateDef struct {
	DevicePlatform string             `json:"device_platform"`
	TemplateValues []FSMTemplateValue `json:"templates_values"`
}

// 聚合后的格式化条目
type FormattedItem struct {
	DeviceName    string      `json:"device_name"`
	InfoFormatted interface{} `json:"info_formatted"`
}

// 响应统计与失败信息
type DeviceFailure struct {
	DeviceIP       string `json:"device_ip"`
	DeviceName     string `json:"device_name"`
	DevicePlatform string `json:"device_platform"`
	Error          string `json:"error"`
}
type DeviceCommandFailures struct {
	DeviceIP       string   `json:"device_ip"`
	DeviceName     string   `json:"device_name"`
	DevicePlatform string   `json:"device_platform"`
	FailedCommands []string `json:"failed_commands"`
	FailedRatio    string   `json:"failed_ratio,omitempty"`
}

// FSM 模版未匹配信息
type DeviceTemplateNotFound struct {
	DeviceName       string   `json:"device_name"`
	DevicePlatform   string   `json:"device_platform"`
	NotFoundCommands []string `json:"notfound_commands"`
	NotFoundRatio    string   `json:"notfound_ratio"`
}

type FormatBatchResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// 前缀到设备名上一层：/{minio_prefix}/{save_dir}/{task_id}/formatted/
	JSONPrefix      string                   `json:"json_prefix"`
	DateTime        string                   `json:"date_time"` // YYYYMMDD_HHMMSS
	LoginFailures   []DeviceFailure          `json:"login_failures"`
	CollectFailures []DeviceCommandFailures  `json:"collect_failures"`
	FormatFailures  []DeviceCommandFailures  `json:"failed_commands"`
	FSMNotFound     []DeviceTemplateNotFound `json:"fsm_notfound"`
	Stats           struct {
		TotalDevices  int `json:"total_devices"`
		FullySuccess  int `json:"fully_success_devices"`
		LoginFailed   int `json:"login_failed_devices"`
		CollectFailed int `json:"collect_failed_devices"`
		ParseFailed   int `json:"parse_failed_devices"`
	} `json:"stats"`
	Stored []StoredObject `json:"stored_objects,omitempty"`
}

// ====== 快速格式化请求/响应 ======
// 设计目标：复用登录与采集能力，低耦合，仅返回 JSON 结果，不强制写入 MinIO

// FormatFastRequest 针对单台设备的快速格式化请求
type FormatFastRequest struct {
	TaskID       string             `json:"task_id"`
	TaskName     string             `json:"task_name,omitempty"`
	RetryFlag    *int               `json:"retry_flag,omitempty"` // 仅用于采集重试，解析只进行一次
	Timeout      *int               `json:"timeout,omitempty"`
	Device       []FormatFastDevice `json:"device"` // 允许传入一个设备（数组便于扩展）
	FSMTemplates []FSMTemplateDef   `json:"fsm_templates,omitempty"`
}

// FormatFastDevice 快速格式化设备参数（支持单条命令或命令列表）
type FormatFastDevice struct {
	DeviceIP        string   `json:"device_ip"`
	DevicePort      int      `json:"device_port,omitempty"`
	DeviceName      string   `json:"device_name"`
	DevicePlatform  string   `json:"device_platform"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	Cli             string   `json:"cli,omitempty"`
	CliList         []string `json:"cli_list,omitempty"`
}

// FormatFastResponse 快速格式化响应
// result: success | collect_failed | formatted_failed
type FormatFastResponse struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	TaskID   string `json:"task_id"`
	DateTime string `json:"date_time"`
	Result   string `json:"result"`
	Device   struct {
		DeviceIP       string `json:"device_ip"`
		DeviceName     string `json:"device_name"`
		DevicePlatform string `json:"device_platform"`
	} `json:"device"`
	Raw       []CommandResultView    `json:"raw"`
	Formatted map[string]interface{} `json:"formatted_json"`
}

// ====== 服务定义 ======

// FormatService 格式化服务。
// 交互说明：所有设备命令执行统一走 InteractBasic（交互优先、失败回退非交互逻辑已内联到 InteractBasic）。
// 预命令与过滤：平台级预命令（enable/关闭分页）及其结果过滤、行级输出过滤均由 InteractBasic 处理；本服务不再重复注入或过滤。
// 作用：负责并发调度、结果聚合与写入，不直接操作 SSH 客户端。

type FormatService struct {
    cfg         *config.Config
    sshPool     *ssh.Pool
    workers     chan struct{}
    interact    *InteractBasic
    minioWriter *FormatMinioWriter
    running     bool
    mutex       sync.RWMutex
}

func NewFormatService(cfg *config.Config) *FormatService {
    conc := cfg.Collector.Concurrent
    if conc <= 0 { conc = 1 }
    threads := cfg.Collector.Threads
    if threads <= 0 { threads = cfg.SSH.MaxSessions }
    poolConfig := &ssh.PoolConfig{
        MaxIdle:     10,
        MaxActive:   conc,
        IdleTimeout: 5 * time.Minute,
        SSHConfig: &ssh.Config{
            Timeout:        cfg.SSH.Timeout,
            ConnectTimeout: cfg.SSH.ConnectTimeout,
            KeepAlive:   cfg.SSH.KeepAliveInterval,
            MaxSessions: threads,
        },
    }

    pool := ssh.NewPool(poolConfig)
    return &FormatService{
        cfg:         cfg,
        sshPool:     pool,
        workers:     make(chan struct{}, conc),
        interact:    NewInteractBasic(cfg, pool),
        minioWriter: NewFormatMinioWriter(cfg),
    }
}

func (s *FormatService) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.running {
		return fmt.Errorf("format service already running")
	}
	s.running = true
	logger.Info("Format service started")
	return nil
}

func (s *FormatService) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !s.running {
		return nil
	}
	s.running = false
	if err := s.sshPool.Close(); err != nil {
		logger.Error("Failed to close SSH pool (format)", "error", err)
	}
	logger.Info("Format service stopped")
	return nil
}

// ExecuteBatch 执行批量格式化流程
func (s *FormatService) ExecuteBatch(ctx context.Context, req *FormatBatchRequest) (*FormatBatchResponse, error) {
	if !s.running {
		return nil, fmt.Errorf("format service is not running")
	}
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if len(req.Devices) == 0 {
		return nil, fmt.Errorf("devices is empty")
	}

	start := time.Now()
	date := start.Format("20060102")
	timeStr := start.Format("150405")
	dateTime := fmt.Sprintf("%s_%s", date, timeStr)

	// 构造模板查找表：platform -> cli -> []fsm_value
	tmpl := make(map[string]map[string][]string)
	for _, d := range req.FSMTemplates {
		p := strings.ToLower(strings.TrimSpace(d.DevicePlatform))
		if p == "" {
			continue
		}
		if _, ok := tmpl[p]; !ok {
			tmpl[p] = make(map[string][]string)
		}
		for _, tv := range d.TemplateValues {
			cli := strings.ToLower(strings.TrimSpace(tv.CLIName))
			if cli == "" {
				continue
			}
			tmpl[p][cli] = append(tmpl[p][cli], tv.FSMValue)
		}
	}

	// 聚合：platform -> cli -> []FormattedItem
	agg := make(map[string]map[string][]FormattedItem)

	// 失败统计
	loginFailures := make([]DeviceFailure, 0)
	collectFailures := make([]DeviceCommandFailures, 0)
	formatFailures := make([]DeviceCommandFailures, 0)
	fsmNotFound := make([]DeviceTemplateNotFound, 0)

	// 并发控制
	k := s.cfg.Collector.Concurrent
	if k <= 0 {
		k = 1
	}
	sem := make(chan struct{}, k)
	var wg sync.WaitGroup
	muAgg := &sync.Mutex{}

	for _, dev := range req.Devices {
		dev := dev // capture
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 限制并发
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

            // 执行采集（仅采集重试，解析仅在成功采集后进行一次）
            timeout := s.effectiveTimeout(req.Timeout, dev.DevicePlatform)
            // 默认回退：平台默认 -> collector.retry_flags
            retries := s.effectiveRetries(req.RetryFlag, dev.DevicePlatform)
            attempts := retries + 1
            var res []*ssh.CommandResult
            var err error
            for try := 0; try < attempts; try++ {
                res, err = s.interact.Execute(ctx, &ExecRequest{
                    DeviceIP:        dev.DeviceIP,
                    Port:            dev.DevicePort,
                    DeviceName:      dev.DeviceName,
                    DevicePlatform:  dev.DevicePlatform,
                    CollectProtocol: dev.CollectProtocol,
                    UserName:        dev.UserName,
                    Password:        dev.Password,
                    EnablePassword:  dev.EnablePassword,
                    TimeoutSec:      timeout,
                }, dev.CliList)
				if err == nil {
					break
				}
				// 若还有剩余重试次数则继续；否则记录失败并结束
				if try+1 >= attempts {
					loginFailures = append(loginFailures, DeviceFailure{
						DeviceIP:       dev.DeviceIP,
						DeviceName:     dev.DeviceName,
						DevicePlatform: dev.DevicePlatform,
						Error:          err.Error(),
					})
					return
				}
			}

            // 统一交互层已过滤预命令与应用行过滤，此处直接使用结果
            filtered := res

			// 统计/聚合失败命令
			failedCmds := make([]string, 0)

			// 写入原始数据（每设备每命令）
			for i, r := range filtered {
				if r == nil {
					continue
				}
				if r.ExitCode != 0 || strings.TrimSpace(r.Error) != "" {
					failedCmds = append(failedCmds, safeDisplayCmd(dev.CliList, i))
				}
				// 原始数据对象路径：/{minio_prefix}/{save_dir}/{task_id}/raw/{batch_id}/{device_name}/formatted/{cli_name}.txt
				cli := strings.ToLower(strings.TrimSpace(safeDisplayCmd(dev.CliList, i)))
				obj := s.buildRawObjectPath(req.SaveDir, req.TaskID, req.TaskBatch, dev.DeviceName, cli)
				if obj != "" {
					if _, werr := s.minioWriter.PutObject(ctx, obj, []byte(r.Output), "text/plain; charset=utf-8"); werr != nil {
						logger.Warn("Write raw to MinIO failed", "device", dev.DeviceName, "cmd", cli, "error", werr)
					}
				}
			}
			if len(failedCmds) > 0 {
				collectFailures = append(collectFailures, DeviceCommandFailures{
					DeviceIP:       dev.DeviceIP,
					DeviceName:     dev.DeviceName,
					DevicePlatform: dev.DevicePlatform,
					FailedCommands: failedCmds,
				})
			}

			// 应用 FSM 模板并聚合
			p := strings.ToLower(strings.TrimSpace(dev.DevicePlatform))
			totalCmds := len(filtered)
			notfoundCmds := make([]string, 0)
			parseFailedCmds := make([]string, 0)
			for i, r := range filtered {
				if r == nil {
					continue
				}
				cli := strings.ToLower(strings.TrimSpace(safeDisplayCmd(dev.CliList, i)))
				// 模板列表
				tvals := tmpl[p][cli]
				formatted, ferr := s.applyFSM(tvals, r.Output)
				if ferr != nil {
					// 区分未匹配模板与解析失败
					if len(tvals) == 0 || strings.Contains(strings.ToLower(ferr.Error()), "no matched fsm template") {
						notfoundCmds = append(notfoundCmds, safeDisplayCmd(dev.CliList, i))
						formatted = map[string]interface{}{"parsed": []interface{}{}}
					} else {
						parseFailedCmds = append(parseFailedCmds, safeDisplayCmd(dev.CliList, i))
						formatted = map[string]interface{}{"parsed": []interface{}{}}
					}
				}
				muAgg.Lock()
				if _, ok := agg[p]; !ok {
					agg[p] = make(map[string][]FormattedItem)
				}
				agg[p][cli] = append(agg[p][cli], FormattedItem{DeviceName: dev.DeviceName, InfoFormatted: formatted})
				muAgg.Unlock()
			}
			// 聚合：未匹配模板统计
			if len(notfoundCmds) > 0 {
				ratio := fmt.Sprintf("%d/%d", len(notfoundCmds), max(1, totalCmds))
				fsmNotFound = append(fsmNotFound, DeviceTemplateNotFound{
					DeviceName:       dev.DeviceName,
					DevicePlatform:   dev.DevicePlatform,
					NotFoundCommands: notfoundCmds,
					NotFoundRatio:    ratio,
				})
			}
			// 聚合：解析失败统计
			if len(parseFailedCmds) > 0 {
				ratio := fmt.Sprintf("%d/%d", len(parseFailedCmds), max(1, totalCmds))
				formatFailures = append(formatFailures, DeviceCommandFailures{
					DeviceIP:       dev.DeviceIP,
					DeviceName:     dev.DeviceName,
					DevicePlatform: dev.DevicePlatform,
					FailedCommands: parseFailedCmds,
					FailedRatio:    ratio,
				})
			}
		}()
	}
	wg.Wait()

	// 写入聚合 JSON
	stored := make([]StoredObject, 0)
	for platform, byCmd := range agg {
		for cli, items := range byCmd {
			// 采用缩进美化输出，便于人工阅读与比对
			data, _ := json.MarshalIndent(items, "", "  ")
			obj := s.buildFormattedJSONPath(req.SaveDir, req.TaskID, platform, cli, req.TaskBatch)
			if obj == "" {
				continue
			}
			if so, err := s.minioWriter.PutObject(ctx, obj, data, "application/json; charset=utf-8"); err != nil {
				logger.Warn("Write formatted JSON failed", "obj", obj, "error", err)
			} else {
				stored = append(stored, so)
			}
		}
	}

	// 统计与响应
	resp := &FormatBatchResponse{
		Code:            "SUCCESS",
		Message:         "批量格式化处理完成",
		JSONPrefix:      s.buildJSONPrefix(req.SaveDir, req.TaskID),
		DateTime:        dateTime,
		LoginFailures:   loginFailures,
		CollectFailures: collectFailures,
		FormatFailures:  formatFailures,
		Stored:          stored,
	}
	resp.Stats.TotalDevices = len(req.Devices)
	resp.Stats.LoginFailed = len(loginFailures)
	resp.Stats.CollectFailed = uniqueDeviceCount(collectFailures)
	// 解析失败设备数：未匹配模板与解析失败的并集
	resp.Stats.ParseFailed = unionParseFailedDevicesCount(formatFailures, fsmNotFound)
	resp.Stats.FullySuccess = resp.Stats.TotalDevices - resp.Stats.LoginFailed - resp.Stats.ParseFailed
	resp.FSMNotFound = fsmNotFound

	return resp, nil
}

// ExecuteFast 针对单台设备的快速格式化流程
// 仅在采集成功后进行一次解析；采集阶段按 retry_flag 进行重试
func (s *FormatService) ExecuteFast(ctx context.Context, req *FormatFastRequest) (*FormatFastResponse, error) {
	if !s.running {
		return nil, fmt.Errorf("format service is not running")
	}
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if len(req.Device) == 0 {
		return nil, fmt.Errorf("device is empty")
	}

	start := time.Now()
	date := start.Format("20060102")
	timeStr := start.Format("150405")
	dateTime := fmt.Sprintf("%s_%s", date, timeStr)

	dev := req.Device[0]
	// 兼容单条命令与命令列表
	userCmds := make([]string, 0, max(1, len(dev.CliList)))
	if strings.TrimSpace(dev.Cli) != "" {
		userCmds = []string{dev.Cli}
	} else if len(dev.CliList) > 0 {
		userCmds = append(userCmds, dev.CliList...)
	}
	if len(userCmds) == 0 {
		return nil, fmt.Errorf("cli or cli_list is required")
	}

	// 构造模板查找表：platform -> cli -> []fsm_value
	tmpl := make(map[string]map[string][]string)
	for _, d := range req.FSMTemplates {
		p := strings.ToLower(strings.TrimSpace(d.DevicePlatform))
		if p == "" {
			continue
		}
		if _, ok := tmpl[p]; !ok {
			tmpl[p] = make(map[string][]string)
		}
		for _, tv := range d.TemplateValues {
			cli := strings.ToLower(strings.TrimSpace(tv.CLIName))
			if cli == "" {
				continue
			}
			tmpl[p][cli] = append(tmpl[p][cli], tv.FSMValue)
		}
	}

    // 执行采集（仅采集重试，解析仅在成功采集后进行一次）
    timeout := s.effectiveTimeout(req.Timeout, dev.DevicePlatform)
    // 默认回退：平台默认 -> collector.retry_flags
    retries := s.effectiveRetries(req.RetryFlag, dev.DevicePlatform)
    attempts := retries + 1
    var res []*ssh.CommandResult
    var err error
    for try := 0; try < attempts; try++ {
    res, err = s.interact.Execute(ctx, &ExecRequest{
        DeviceIP:        dev.DeviceIP,
        Port:            dev.DevicePort,
        DeviceName:      dev.DeviceName,
        DevicePlatform:  dev.DevicePlatform,
        CollectProtocol: dev.CollectProtocol,
        UserName:        dev.UserName,
        Password:        dev.Password,
        EnablePassword:  dev.EnablePassword,
        TimeoutSec:      timeout,
    }, userCmds)
		if err == nil {
			break
		}
		if try+1 >= attempts {
			// 采集失败：返回 collect_failed
			resp := &FormatFastResponse{Code: "SUCCESS", Message: "快速格式化处理完成", TaskID: req.TaskID, DateTime: dateTime, Result: "collect_failed"}
			resp.Device.DeviceIP = dev.DeviceIP
			resp.Device.DeviceName = dev.DeviceName
			resp.Device.DevicePlatform = dev.DevicePlatform
			resp.Raw = []CommandResultView{}
			resp.Formatted = map[string]interface{}{}
			return resp, nil
		}
	}

    // 统一交互层已过滤预命令与应用行过滤，此处直接使用结果
    filtered := res

	// 原始采集信息
	rawViews := make([]CommandResultView, 0, len(filtered))
	nonEmptyRaw := 0
	for i, r := range filtered {
		if r == nil {
			continue
		}
		if strings.TrimSpace(r.Output) != "" {
			nonEmptyRaw++
		}
		rawViews = append(rawViews, CommandResultView{
			Command:      safeDisplayCmd(userCmds, i),
			RawOutput:    r.Output,
			FormatOutput: nil,
			Error:        r.Error,
			ExitCode:     r.ExitCode,
			DurationMS:   r.Duration.Milliseconds(),
		})
	}

	// 采集结果为空
	if len(rawViews) == 0 || nonEmptyRaw == 0 {
		resp := &FormatFastResponse{Code: "SUCCESS", Message: "快速格式化处理完成", TaskID: req.TaskID, DateTime: dateTime, Result: "collect_failed"}
		resp.Device.DeviceIP = dev.DeviceIP
		resp.Device.DeviceName = dev.DeviceName
		resp.Device.DevicePlatform = dev.DevicePlatform
		resp.Raw = rawViews
		resp.Formatted = map[string]interface{}{}
		return resp, nil
	}

	// 应用 FSM
	p := strings.ToLower(strings.TrimSpace(dev.DevicePlatform))
	formatted := make(map[string]interface{})
	emptyCount := 0
	for i, r := range filtered {
		if r == nil {
			continue
		}
		cli := strings.ToLower(strings.TrimSpace(safeDisplayCmd(userCmds, i)))
		tvals := tmpl[p][cli]
		f, ferr := s.applyFSM(tvals, r.Output)
		if ferr != nil {
			// 无匹配模板或解析失败，统一按空 parsed 输出
			f = map[string]interface{}{"parsed": []interface{}{}}
		}
		// 判断是否为空
		if mv, ok := f.(map[string]interface{}); ok {
			if arr, ok2 := mv["parsed"].([]interface{}); ok2 {
				if len(arr) == 0 {
					emptyCount++
				}
			} else {
				emptyCount++
			}
		} else {
			emptyCount++
		}
		formatted[cli] = f
	}

	// 解析产物为空
	result := "success"
	if emptyCount >= len(filtered) {
		result = "formatted_failed"
	}

	resp := &FormatFastResponse{Code: "SUCCESS", Message: "快速格式化处理完成", TaskID: req.TaskID, DateTime: dateTime, Result: result}
	resp.Device.DeviceIP = dev.DeviceIP
	resp.Device.DeviceName = dev.DeviceName
	resp.Device.DevicePlatform = dev.DevicePlatform
	resp.Raw = rawViews
	resp.Formatted = formatted
	return resp, nil
}

func (s *FormatService) effectiveTimeout(reqTimeout *int, platform string) int {
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
func (s *FormatService) effectiveRetries(reqRetries *int, platform string) int {
    if reqRetries != nil && *reqRetries >= 0 {
        return *reqRetries
    }
    p := strings.ToLower(strings.TrimSpace(platform))
    if p == "" {
        p = "default"
    }
    d := getPlatformDefaults(p)
    if d.Retries > 0 {
        return d.Retries
    }
    if s.cfg != nil && s.cfg.Collector.RetryFlags > 0 {
        return s.cfg.Collector.RetryFlags
    }
    return 0
}

// 说明：预命令过滤已由统一交互层完成，FormatService 不再重复过滤

func (s *FormatService) applyFSM(templates []string, raw string) (interface{}, error) {
	// FSM 解析逻辑：
	// 1) 支持 TextFSM 风格（Value/Start 与 ${VAR} 占位符），按变量定义编译规则为捕获组
	// 2) 回退：按行编译正则（无法编译则字面匹配），产出匹配明细

	if len(templates) == 0 {
		return nil, fmt.Errorf("no matched fsm template")
	}

	for _, tpl := range templates {
		// 优先尝试 TextFSM 风格：完整状态机语义
		if looksLikeTextFSM(tpl) {
			if tmpl := parseTextFSMTemplate(tpl); tmpl != nil && len(tmpl.states) > 0 {
				recs := runTextFSM(tmpl, strings.Split(raw, "\n"))
				if len(recs) > 0 {
					return map[string]interface{}{"parsed": recs}, nil
				}
			}
			// 次优：简化版规则（单行匹配）
			rules := compileTextFSMRules(tpl)
			if len(rules) > 0 {
				out := parseWithTextFSM(rules, raw)
				if len(out) > 0 {
					return map[string]interface{}{"parsed": out}, nil
				}
			}
			// 若 TextFSM 未产生结果，继续尝试回退逻辑
		}

		// 回退：逐行正则匹配
		regs := compileFSMTemplateRegexes(tpl)
		if len(regs) == 0 {
			continue
		}
		matches := parseByRegexes(regs, raw)
		if len(matches) > 0 {
			return map[string]interface{}{"parsed": matches}, nil
		}
	}
	return nil, fmt.Errorf("fsm parse produced no formatted data")
}

// 将 FSM 模版按行编译为正则表达式。若行无法编译为正则，则按字面值匹配（转义后编译）。
func compileFSMTemplateRegexes(tpl string) []*regexp.Regexp {
	regs := make([]*regexp.Regexp, 0)
	for _, ln := range strings.Split(tpl, "\n") {
		p := strings.TrimSpace(ln)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		// 尝试编译为正则；失败则按字面匹配
		r, err := regexp.Compile(p)
		if err != nil {
			r, err = regexp.Compile(regexp.QuoteMeta(p))
		}
		if err == nil {
			regs = append(regs, r)
		}
	}
	return regs
}

// 根据编译后的正则在原始文本中查找匹配，产出匹配明细（包含行号与分组）。
func parseByRegexes(regexes []*regexp.Regexp, raw string) []map[string]interface{} {
	if len(regexes) == 0 || strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]map[string]interface{}, 0)
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		t := line
		for _, r := range regexes {
			if loc := r.FindStringIndex(t); loc != nil {
				m := r.FindStringSubmatch(t)
				entry := map[string]interface{}{
					"pattern": r.String(),
					"line":    i + 1,
					"match":   t[loc[0]:loc[1]],
				}
				if len(m) > 1 { // 额外捕获组
					entry["groups"] = m[1:]
				}
				out = append(out, entry)
			}
		}
	}
	return out
}

// ====== TextFSM 支持：解析变量定义与规则，并编译占位符为捕获组 ======

type textFSMRule struct {
	regex     *regexp.Regexp
	varOrder  []string
	action    string // Continue | Record | Next
	nextState string
}

// looksLikeTextFSM 简单判定模板是否为 TextFSM 风格
func looksLikeTextFSM(tpl string) bool {
	t := strings.ToLower(tpl)
	return strings.Contains(t, "value ") || strings.Contains(t, "${")
}

// compileTextFSMRules 将 TextFSM 模板编译为规则（支持 Value 定义与 ${VAR} 占位符）
func compileTextFSMRules(tpl string) []textFSMRule {
	lines := strings.Split(tpl, "\n")
	// 解析变量定义：Value NAME (REGEX)
	vars := map[string]string{}
	valRe := regexp.MustCompile(`^\s*Value\s+([A-Za-z_][A-Za-z0-9_]*)\s*\((.+)\)\s*$`)
	for _, ln := range lines {
		l := strings.TrimSpace(ln)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		m := valRe.FindStringSubmatch(l)
		if len(m) == 3 {
			name := m[1]
			pattern := m[2]
			if strings.TrimSpace(pattern) == "" {
				pattern = ".+"
			}
			vars[name] = pattern
		}
	}

	// 解析规则行：形如 "^... ${VAR} ... -> ..."，仅取箭头左侧
	rules := make([]textFSMRule, 0)
	phRe := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	for _, ln := range lines {
		raw := strings.TrimSpace(ln)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(raw), "value ") {
			continue
		}
		// 仅处理含占位符或以 ^ 开头的规则行
		if !(strings.Contains(raw, "${") || strings.HasPrefix(raw, "^")) {
			continue
		}
		// 去掉状态跳转标记
		left := raw
		if i := strings.Index(raw, "->"); i >= 0 {
			left = strings.TrimSpace(raw[:i])
		}
		if left == "" {
			continue
		}
		// 替换 ${VAR} 为捕获组，并记录变量顺序
		varOrder := make([]string, 0)
		buf := strings.Builder{}
		last := 0
		idxs := phRe.FindAllStringSubmatchIndex(left, -1)
		for _, loc := range idxs {
			start, end := loc[0], loc[1]
			nstart, nend := loc[2], loc[3]
			buf.WriteString(left[last:start])
			name := left[nstart:nend]
			pat := vars[name]
			if strings.TrimSpace(pat) == "" {
				pat = ".+"
			}
			buf.WriteString("(" + pat + ")")
			varOrder = append(varOrder, name)
			last = end
		}
		buf.WriteString(left[last:])
		// 编译正则
		r, err := regexp.Compile(buf.String())
		if err != nil {
			// 若编译失败，尝试字面匹配（减少误判）
			r, err = regexp.Compile(regexp.QuoteMeta(buf.String()))
			if err != nil {
				continue
			}
		}
		rules = append(rules, textFSMRule{regex: r, varOrder: varOrder})
	}
	return rules
}

// parseWithTextFSM 按规则在原始文本中查找匹配，返回变量映射
func parseWithTextFSM(rules []textFSMRule, raw string) []map[string]interface{} {
	if len(rules) == 0 || strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]map[string]interface{}, 0)
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		t := line
		for _, rl := range rules {
			m := rl.regex.FindStringSubmatch(t)
			if len(m) > 0 {
				entry := map[string]interface{}{"line": i + 1}
				// 子匹配 1..n 与变量顺序一一对应
				for k := 0; k < len(rl.varOrder) && (k+1) < len(m); k++ {
					entry[rl.varOrder[k]] = m[k+1]
				}
				out = append(out, entry)
			}
		}
	}
	return out
}

// -------- Full TextFSM semantics (State machine, Required, Filldown, List) ---------
type textFSMVar struct {
	name     string
	pattern  string
	required bool
	filldown bool
	list     bool
}

type textFSMTemplate struct {
	vars       map[string]*textFSMVar
	states     map[string][]textFSMRule
	startState string
	ignoreCase bool
}

func parseTextFSMTemplate(tpl string) *textFSMTemplate {
	lines := strings.Split(tpl, "\n")
	tmpl := &textFSMTemplate{
		vars:       map[string]*textFSMVar{},
		states:     map[string][]textFSMRule{},
		startState: "Start",
		ignoreCase: false,
	}
	currentState := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Options (IgnoreCase)
		if strings.HasPrefix(strings.ToLower(line), "options") {
			if strings.Contains(strings.ToLower(line), "ignorecase") {
				tmpl.ignoreCase = true
			}
			continue
		}
		// Value [Required] [Filldown] [List] NAME (REGEX)
		if strings.HasPrefix(line, "Value ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "Value "))
			lp := strings.LastIndex(rest, "(")
			rp := strings.LastIndex(rest, ")")
			if lp == -1 || rp == -1 || rp < lp {
				continue
			}
			head := strings.TrimSpace(rest[:lp])
			pattern := strings.TrimSpace(rest[lp+1 : rp])
			toks := strings.Fields(head)
			if len(toks) == 0 {
				continue
			}
			name := toks[len(toks)-1]
			opts := map[string]bool{"required": false, "filldown": false, "list": false}
			for _, t := range toks[:len(toks)-1] {
				switch strings.ToLower(t) {
				case "required":
					opts["required"] = true
				case "filldown":
					opts["filldown"] = true
				case "list":
					opts["list"] = true
				}
			}
			tmpl.vars[name] = &textFSMVar{name: name, pattern: pattern, required: opts["required"], filldown: opts["filldown"], list: opts["list"]}
			continue
		}
		// States
		if line == "Start" {
			currentState = "Start"
			tmpl.startState = "Start"
			if _, ok := tmpl.states[currentState]; !ok {
				tmpl.states[currentState] = []textFSMRule{}
			}
			continue
		}
		if strings.HasPrefix(line, "State ") {
			currentState = strings.TrimSpace(strings.TrimPrefix(line, "State "))
			if _, ok := tmpl.states[currentState]; !ok {
				tmpl.states[currentState] = []textFSMRule{}
			}
			continue
		}
		// Rules in current state
		if currentState != "" {
			pat := line
			action := "Continue"
			nextState := ""
			if strings.Contains(line, "->") {
				parts := strings.SplitN(line, "->", 2)
				pat = strings.TrimSpace(parts[0])
				act := strings.TrimSpace(parts[1])
				actToks := strings.Fields(act)
				if len(actToks) >= 1 {
					a0 := strings.ToLower(actToks[0])
					switch a0 {
					case "continue":
						action = "Continue"
					case "record":
						action = "Record"
						if len(actToks) >= 2 {
							nextState = actToks[1]
						}
					default:
						action = "Next"
						nextState = actToks[0]
					}
				}
			}
			// Replace ${VAR}
			varOrder := []string{}
			built := pat
			for {
				idx := strings.Index(built, "${")
				if idx == -1 {
					break
				}
				end := strings.Index(built[idx:], "}")
				if end == -1 {
					break
				}
				endIdx := idx + end + 1
				varName := strings.TrimSpace(built[idx+2 : endIdx-1])
				varOrder = append(varOrder, varName)
				patn := ".+"
				if vdef, ok := tmpl.vars[varName]; ok && strings.TrimSpace(vdef.pattern) != "" {
					patn = vdef.pattern
				}
				built = built[:idx] + "(" + patn + ")" + built[endIdx:]
			}
			if tmpl.ignoreCase {
				built = "(?i)" + built
			}
			re, err := regexp.Compile(built)
			if err != nil {
				continue
			}
			tmpl.states[currentState] = append(tmpl.states[currentState], textFSMRule{regex: re, varOrder: varOrder, action: action, nextState: nextState})
		}
	}
	if len(tmpl.states) == 0 {
		return nil
	}
	return tmpl
}

func runTextFSM(tmpl *textFSMTemplate, lines []string) []map[string]interface{} {
	if tmpl == nil || len(tmpl.states) == 0 {
		return nil
	}
	lastVals := map[string]interface{}{}
	records := make([]map[string]interface{}, 0)
	produced := false
	state := tmpl.startState
	for _, line := range lines {
		rules := tmpl.states[state]
		matched := false
		currVals := map[string]interface{}{}
		for _, r := range rules {
			m := r.regex.FindStringSubmatch(line)
			if len(m) == 0 {
				continue
			}
			matched = true
			for i, v := range r.varOrder {
				if i+1 < len(m) {
					val := strings.TrimSpace(m[i+1])
					if vdef, ok := tmpl.vars[v]; ok && vdef.list {
						if arr, ok2 := currVals[v].([]string); ok2 {
							currVals[v] = append(arr, val)
						} else if arr2, ok2 := lastVals[v].([]string); ok2 {
							lastVals[v] = append(arr2, val)
						} else {
							currVals[v] = []string{val}
						}
					} else {
						currVals[v] = val
					}
				}
			}
			switch r.action {
			case "Record":
				rec := map[string]interface{}{}
				missing := false
				for name, vdef := range tmpl.vars {
					var val interface{}
					if cv, ok := currVals[name]; ok {
						val = cv
					} else if vdef.filldown {
						if lv, ok := lastVals[name]; ok {
							val = lv
						}
					}
					if vdef.required && val == nil {
						missing = true
					}
					if val != nil {
						rec[name] = val
					}
				}
				if !missing {
					records = append(records, rec)
					produced = true
				}
				for name, vdef := range tmpl.vars {
					if vdef.filldown {
						if cv, ok := currVals[name]; ok {
							lastVals[name] = cv
						}
					}
				}
				if r.nextState != "" {
					state = r.nextState
				}
			case "Next":
				for name, vdef := range tmpl.vars {
					if vdef.filldown {
						if cv, ok := currVals[name]; ok {
							lastVals[name] = cv
						}
					}
				}
				if r.nextState != "" {
					state = r.nextState
				}
			default: // Continue
				for name, vdef := range tmpl.vars {
					if vdef.filldown {
						if cv, ok := currVals[name]; ok {
							lastVals[name] = cv
						}
					}
				}
			}
		}
		// Fallback: if template has no explicit Record, emit matched values
		if matched && !produced {
			rec := map[string]interface{}{}
			for name := range tmpl.vars {
				if cv, ok := currVals[name]; ok {
					rec[name] = cv
				} else if lv, ok := lastVals[name]; ok {
					rec[name] = lv
				}
			}
			if len(rec) > 0 {
				records = append(records, rec)
			}
		}
	}
	return records
}

func uniqueDeviceCount(items []DeviceCommandFailures) int {
	if len(items) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, it := range items {
		key := strings.ToLower(strings.TrimSpace(it.DeviceIP + "|" + it.DeviceName))
		set[key] = struct{}{}
	}
	return len(set)
}

// 解析失败设备数并集（包含未匹配模板）
func unionParseFailedDevicesCount(fails []DeviceCommandFailures, notfound []DeviceTemplateNotFound) int {
	if len(fails) == 0 && len(notfound) == 0 {
		return 0
	}
	set := map[string]struct{}{}
	for _, it := range fails {
		key := strings.ToLower(strings.TrimSpace(it.DevicePlatform + "|" + it.DeviceName))
		set[key] = struct{}{}
	}
	for _, nf := range notfound {
		key := strings.ToLower(strings.TrimSpace(nf.DevicePlatform + "|" + nf.DeviceName))
		set[key] = struct{}{}
	}
	return len(set)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func safeDisplayCmd(cliList []string, idx int) string {
	if idx >= 0 && idx < len(cliList) {
		return cliList[idx]
	}
	return ""
}

// ====== MinIO 写入器（格式化路径语义） ======

type FormatMinioWriter struct {
	cfg      *config.Config
	client   *minio.Client
	endpoint string
	ensured  bool
}

func NewFormatMinioWriter(cfg *config.Config) *FormatMinioWriter {
	host := strings.TrimSpace(cfg.Storage.Minio.Host)
	port := cfg.Storage.Minio.Port
	if host == "" || port <= 0 {
		logger.Warn("MinIO configuration incomplete for format service")
		return nil
	}
	endpoint := fmt.Sprintf("%s:%d", host, port)
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
	}
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.Storage.Minio.AccessKey, cfg.Storage.Minio.SecretKey, ""),
		Secure:    cfg.Storage.Minio.Secure,
		Transport: transport,
	})
	if err != nil {
		logger.Error("MinIO client init failed (format)", "error", err)
		return nil
	}
	w := &FormatMinioWriter{cfg: cfg, client: cli, endpoint: endpoint}
	// 尝试确保 bucket
	bucket := strings.TrimSpace(cfg.Storage.Minio.Bucket)
	if bucket != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := w.ensureBucket(ctx, bucket, 2); err == nil {
			w.ensured = true
		} else {
			logger.Warn("MinIO bucket ensure failed (format)", "error", err)
		}
	}
	return w
}

func (w *FormatMinioWriter) PutObject(parent context.Context, objectName string, data []byte, contentType string) (StoredObject, error) {
	if w == nil || w.client == nil {
		return StoredObject{}, fmt.Errorf("minio client not initialized")
	}
	bucket := strings.TrimSpace(w.cfg.Storage.Minio.Bucket)
	if bucket == "" {
		return StoredObject{}, fmt.Errorf("minio bucket not configured")
	}

	// 写入前快速连通性检查
	if err := w.fastCheck(parent); err != nil {
		return StoredObject{}, fmt.Errorf("minio connectivity failed to %s: %w", w.endpoint, err)
	}
	if !w.ensured {
		if err := w.ensureBucket(parent, bucket, 3); err != nil {
			return StoredObject{}, fmt.Errorf("minio ensure bucket failed: %w", err)
		}
		w.ensured = true
	}
	ct := contentType
	if strings.TrimSpace(ct) == "" {
		ct = "application/octet-stream"
	}

	var lastErr error
	attempts := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i := 0; i < len(attempts); i++ {
		r := bytes.NewReader(data)
		attemptCtx, cancel := w.attemptContext(parent, attempts[i])
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

	return StoredObject{URI: "minio://" + path.Join(bucket, objectName), Size: int64(len(data)), ContentType: ct}, nil
}

func (w *FormatMinioWriter) fastCheck(parent context.Context) error {
	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(parent, "tcp", w.endpoint)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func (w *FormatMinioWriter) ensureBucket(parent context.Context, bucket string, retries int) error {
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
		if exists {
			return nil
		}
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
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("bucket ensure failed for %s", bucket)
}

func (w *FormatMinioWriter) attemptContext(parent context.Context, prefer time.Duration) (context.Context, context.CancelFunc) {
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

// ====== 路径构造工具 ======

func (s *FormatService) buildJSONPrefix(saveDir, taskID string) string {
	prefix := strings.TrimSpace(s.cfg.DataFormat.MinioPrefix)
	if prefix == "" {
		prefix = "data-formats"
	}
	parts := []string{"", prefix}
	if sd := strings.TrimSpace(saveDir); sd != "" {
		parts = append(parts, sd)
	}
	if tid := strings.TrimSpace(taskID); tid != "" {
		parts = append(parts, tid)
	}
	parts = append(parts, "formatted")
	return path.Join(parts...) + "/"
}

func (s *FormatService) buildFormattedJSONPath(saveDir, taskID, platform, cli string, batchID int) string {
	prefix := strings.TrimSpace(s.cfg.DataFormat.MinioPrefix)
	if prefix == "" {
		prefix = "data-formats"
	}
	p := strings.ToLower(strings.TrimSpace(platform))
	c := slug(cli)
	bid := batchID
	if bid <= 0 {
		bid = 1
	}
	// /{minio_prefix}/{save_dir}/{task_id}/formatted/{device_platform}/{cli_name}/formatted_{batch_id}.json
	parts := []string{"", prefix}
	if sd := strings.TrimSpace(saveDir); sd != "" {
		parts = append(parts, sd)
	}
	if tid := strings.TrimSpace(taskID); tid != "" {
		parts = append(parts, tid)
	}
	parts = append(parts, "formatted", p, c)
	fname := fmt.Sprintf("formatted_%d.json", bid)
	return path.Join(path.Join(parts...), fname)
}

func (s *FormatService) buildRawObjectPath(saveDir, taskID string, batchID int, deviceName, cli string) string {
	prefix := strings.TrimSpace(s.cfg.DataFormat.MinioPrefix)
	if prefix == "" {
		prefix = "data-formats"
	}
	dn := slug(deviceName)
	c := slug(cli)
	bid := batchID
	if bid <= 0 {
		bid = 1
	}
	// /{minio_prefix}/{save_dir}/{task_id}/raw/{batch_id}/{device_name}/formatted/{cli_name}.txt
	parts := []string{"", prefix}
	if sd := strings.TrimSpace(saveDir); sd != "" {
		parts = append(parts, sd)
	}
	if tid := strings.TrimSpace(taskID); tid != "" {
		parts = append(parts, tid)
	}
	parts = append(parts, "raw", fmt.Sprintf("%d", bid), dn, "formatted")
	fname := c + ".txt"
	return path.Join(path.Join(parts...), fname)
}
