package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Request/Device structures for custom batch
type CustomerDevice struct {
	DeviceIP        string   `json:"device_ip"`
	Port            int      `json:"port,omitempty"`
	DeviceName      string   `json:"device_name,omitempty"`
	DevicePlatform  string   `json:"device_platform,omitempty"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list,omitempty"`
}

type CustomerBatchRequest struct {
	TaskID    string           `json:"task_id"`
	TaskName  string           `json:"task_name,omitempty"`
	RetryFlag *int             `json:"retry_flag,omitempty"`
	Timeout   *int             `json:"timeout,omitempty"`
	Devices   []CustomerDevice `json:"devices"`
}

// System batch endpoint payloads (per-device CLI list)
type SystemDevice struct {
	DeviceIP        string   `json:"device_ip"`
	Port            int      `json:"port,omitempty"`
	DeviceName      string   `json:"device_name,omitempty"`
	DevicePlatform  string   `json:"device_platform"`
	CollectProtocol string   `json:"collect_protocol,omitempty"`
	UserName        string   `json:"user_name"`
	Password        string   `json:"password"`
	EnablePassword  string   `json:"enable_password,omitempty"`
	CliList         []string `json:"cli_list,omitempty"`
}

type SystemBatchRequest struct {
	TaskID     string         `json:"task_id"`
	TaskName   string         `json:"task_name,omitempty"`
	RetryFlag  *int           `json:"retry_flag,omitempty"`
	Timeout    *int           `json:"timeout,omitempty"`
	DeviceList []SystemDevice `json:"device_list"`
}

// Response structures (partial, enough for trimming raw_output and pretty print)
type CommandResultView struct {
	Command      string      `json:"command"`
	RawOutput    string      `json:"raw_output"`
	FormatOutput interface{} `json:"format_output"`
	Error        string      `json:"error"`
	ExitCode     int         `json:"exit_code"`
	DurationMS   int64       `json:"duration_ms"`
}

type DeviceExecResult struct {
	DeviceIP       string              `json:"device_ip"`
	Port           int                 `json:"port"`
	DeviceName     string              `json:"device_name"`
	DevicePlatform string              `json:"device_platform"`
	TaskID         string              `json:"task_id"`
	Success        bool                `json:"success"`
	Results        []CommandResultView `json:"results"`
	Error          string              `json:"error"`
	DurationMS     int64               `json:"duration_ms"`
	Timestamp      string              `json:"timestamp"`
}

type APIResponse struct {
	Code    string             `json:"code"`
	Message string             `json:"message"`
	Data    []DeviceExecResult `json:"data"`
	Total   int                `json:"total"`
}

// Printed structures with wrapped lines for readability
type CommandResultViewPrinted struct {
	Command        string      `json:"command"`
	RawOutput      string      `json:"raw_output"`
	RawOutputLines []string    `json:"raw_output_lines"`
	FormatOutput   interface{} `json:"format_output"`
	Error          string      `json:"error"`
	ExitCode       int         `json:"exit_code"`
	DurationMS     int64       `json:"duration_ms"`
}

type DeviceExecResultPrinted struct {
	DeviceIP       string                     `json:"device_ip"`
	Port           int                        `json:"port"`
	DeviceName     string                     `json:"device_name"`
	DevicePlatform string                     `json:"device_platform"`
	TaskID         string                     `json:"task_id"`
	Success        bool                       `json:"success"`
	Results        []CommandResultViewPrinted `json:"results"`
	Error          string                     `json:"error"`
	DurationMS     int64                      `json:"duration_ms"`
	Timestamp      string                     `json:"timestamp"`
}

type APIResponsePrinted struct {
	Code    string                    `json:"code"`
	Message string                    `json:"message"`
	Data    []DeviceExecResultPrinted `json:"data"`
	Total   int                       `json:"total"`
}

func trimLines(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	// Normalize CRLF to LF
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) <= limit {
		return s
	}
	trimmed := strings.Join(lines[:limit], "\n")
	return trimmed
}

// wrap a single line by rune count width
func wrapLineByRune(s string, width int) []string {
	if width <= 0 || len(s) == 0 {
		return []string{s}
	}
	rs := []rune(s)
	out := make([]string, 0, (len(rs)/width)+1)
	for i := 0; i < len(rs); i += width {
		end := i + width
		if end > len(rs) {
			end = len(rs)
		}
		out = append(out, string(rs[i:end]))
	}
	return out
}

// build wrapped lines from raw output with overall line limit
func buildWrappedLines(raw string, width int, limit int) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, limit)
	for _, ln := range lines {
		parts := wrapLineByRune(ln, width)
		for _, p := range parts {
			out = append(out, p)
			if limit > 0 && len(out) >= limit {
				return out[:limit]
			}
		}
	}
	return out
}

// kill listening process(es) on a TCP port (macOS using lsof)
func killListeningOnPort(port int) ([]int, error) {
	if port <= 0 {
		return nil, nil
	}
	cmd := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t")
	out, err := cmd.Output()
	if err != nil {
		// if lsof not available or no listeners, return empty
		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	pids := make([]int, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		pid, e := strconv.Atoi(ln)
		if e != nil {
			continue
		}
		pids = append(pids, pid)
	}
	for _, pid := range pids {
		// try SIGTERM, then SIGKILL
		_ = syscall.Kill(pid, syscall.SIGTERM)
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return pids, nil
}

// parse host and port from base server URL, fallback to defaultPort
func parseHostPort(base string, defaultPort int) (string, int) {
	host := "localhost"
	port := defaultPort
	u, err := url.Parse(strings.TrimSpace(base))
	if err == nil {
		if h := u.Hostname(); h != "" {
			host = h
		}
		if ps := u.Port(); ps != "" {
			if p, e := strconv.Atoi(ps); e == nil {
				port = p
			}
		}
	}
	if port <= 0 {
		port = defaultPort
	}
	return host, port
}

func isPortOpen(host string, port int) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 300*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	// fallback candidates for wildcard/unspecified hosts
	candidates := []string{"127.0.0.1", "::1", "localhost"}
	for _, h := range candidates {
		conn, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", h, port), 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func waitForPortReady(host string, port int, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if isPortOpen(host, port) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("port %d not ready within %ds", port, timeoutSec)
}

func startServer(serverMain string) (*exec.Cmd, error) {
	cmd := exec.Command("go", "run", serverMain)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func main() {
	server := flag.String("server", "http://localhost:18000", "Server base URL, e.g. http://localhost:18000")
	path := flag.String("path", "/api/v1/collector/batch/custom", "API path for custom batch collector")
	payloadFile := flag.String("payload", "", "Optional JSON file path to override default payload")
	outFile := flag.String("out", "", "Optional output file to write pretty JSON")
	limit := flag.Int("limit", 20, "Max lines per command raw_output in printed JSON")
	timeout := flag.Int("http_timeout", 120, "HTTP client timeout seconds")
	killPort := flag.Int("kill_port", 18000, "Kill process listening on this port before run (0=disable)")
	wrapWidth := flag.Int("wrap_width", 100, "Auto wrap width per line in raw_output_lines")
	startServerFlag := flag.Bool("start_server", true, "Auto start server if not listening")
	keepServer := flag.Bool("keep_server", false, "Keep started server running after test")
	startTimeout := flag.Int("start_timeout", 10, "Seconds to wait for server port ready")
	serverMain := flag.String("server_main", "cmd/server/main.go", "Path to server main.go to run")
	enablePwd := flag.String("enable_password", "", "Optional enable/privileged password applied to devices")
	flag.Parse()

	// Pre-step: auto kill listening process on port (default 18000)

	// Kill port if requested
	if *killPort > 0 {
		pids, _ := killListeningOnPort(*killPort)
		if len(pids) > 0 {
			fmt.Printf("Killed %d process(es) listening on port %d: %v\n", len(pids), *killPort, pids)
			// brief wait for port release
			time.Sleep(500 * time.Millisecond)
		} else {
			fmt.Printf("No listening process found on port %d or lsof not available.\n", *killPort)
		}
	}

	// Start server if requested and not already listening
	host, port := parseHostPort(*server, *killPort)
	var srvCmd *exec.Cmd
	if *startServerFlag {
		if !isPortOpen(host, port) {
			fmt.Printf("Starting server: go run %s\n", *serverMain)
			var err error
			srvCmd, err = startServer(*serverMain)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
				os.Exit(1)
			}
			if err = waitForPortReady(host, port, *startTimeout); err != nil {
				fmt.Fprintf(os.Stderr, "server not ready: %v\n", err)
				if srvCmd != nil && srvCmd.Process != nil {
					_ = srvCmd.Process.Kill()
				}
				os.Exit(1)
			}
			fmt.Printf("Server listening on %s:%d\n", host, port)
			if !*keepServer {
				defer func() {
					if srvCmd != nil && srvCmd.Process != nil {
						_ = srvCmd.Process.Signal(syscall.SIGTERM)
					}
				}()
			}
		} else {
			fmt.Printf("Detected existing listener on %s:%d, skip start.\n", host, port)
		}
	}

	// Build default payload from provided request template
	rf := 2
	to := 10
	// Detect endpoint type: custom vs system
	isSystem := strings.Contains(*path, "/batch/system")

	// Prepare payloads for both endpoints
	var reqCustom CustomerBatchRequest
	var reqSystem SystemBatchRequest
	if isSystem {
		reqSystem = SystemBatchRequest{
			TaskID:    "Test-2001",
			TaskName:  "system-batch-check",
			RetryFlag: &rf,
			Timeout:   &to,
			DeviceList: []SystemDevice{
				{
					DeviceIP:        "139.196.196.96",
					Port:            21201,
					DeviceName:      "test-out-sw1",
					DevicePlatform:  "cisco_ios",
					CollectProtocol: "ssh",
					UserName:        "eccom123",
					Password:        "Eccom@12345",
					EnablePassword:  *enablePwd,
					CliList:         []string{"show version", "show running-config | include hostname"},
				},
				{
					DeviceIP:        "139.196.196.96",
					Port:            21203,
					DeviceName:      "test-out-r1",
					DevicePlatform:  "h3c_sr",
					CollectProtocol: "ssh",
					UserName:        "eccom123",
					Password:        "Eccom@12345",
					EnablePassword:  *enablePwd,
					CliList:         []string{"display version", "display interface brief"},
				},
			},
		}
	} else {
		reqCustom = CustomerBatchRequest{
			TaskID:    "Test-2001",
			TaskName:  "custom-batch-check",
			RetryFlag: &rf,
			Timeout:   &to,
			Devices: []CustomerDevice{
				{
					DeviceIP:        "139.196.196.96",
					Port:            21201,
					DeviceName:      "test-out-sw1",
					DevicePlatform:  "cisco_ios",
					CollectProtocol: "ssh",
					UserName:        "eccom123",
					Password:        "Eccom@12345",
					EnablePassword:  *enablePwd,
					CliList:         []string{"show version", "show running-config | include hostname"},
				},
				{
					DeviceIP:        "139.196.196.96",
					Port:            21203,
					DeviceName:      "test-out-r1",
					DevicePlatform:  "h3c_sr",
					CollectProtocol: "ssh",
					UserName:        "eccom123",
					Password:        "Eccom@12345",
					EnablePassword:  *enablePwd,
					CliList:         []string{"display version", "display interface brief"},
				},
				{
					DeviceIP:        "139.196.196.96",
					Port:            20204,
					DeviceName:      "test-out-r2",
					DevicePlatform:  "juniper",
					CollectProtocol: "ssh",
					UserName:        "eccom123",
					Password:        "Eccom@12345",
					EnablePassword:  *enablePwd,
					CliList:         []string{"show version", "show configuration | display set | match hostname"},
				},
			},
		}
	}

	var body []byte
	var err error
	if *payloadFile != "" {
		body, err = os.ReadFile(*payloadFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read payload file: %v\n", err)
			os.Exit(1)
		}
	} else {
		if isSystem {
			body, err = json.Marshal(reqSystem)
		} else {
			body, err = json.Marshal(reqCustom)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to build payload: %v\n", err)
			os.Exit(1)
		}
	}

	url := strings.TrimRight(*server, "/") + *path
	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create request: %v\n", err)
		os.Exit(1)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read response: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "HTTP %d\n%s\n", resp.StatusCode, string(respBody))
		os.Exit(2)
	}

	// Decode response and trim/wrap raw_output lines
	var out APIResponse
	if err = json.Unmarshal(respBody, &out); err != nil {
		// Fallback: print raw response when schema mismatch
		fmt.Println(string(respBody))
		os.Exit(0)
	}

	// Build printed response with raw_output_lines
	printed := APIResponsePrinted{Code: out.Code, Message: out.Message, Total: out.Total}
	printed.Data = make([]DeviceExecResultPrinted, 0, len(out.Data))
	for _, d := range out.Data {
		pr := DeviceExecResultPrinted{
			DeviceIP:       d.DeviceIP,
			Port:           d.Port,
			DeviceName:     d.DeviceName,
			DevicePlatform: d.DevicePlatform,
			TaskID:         d.TaskID,
			Success:        d.Success,
			Error:          d.Error,
			DurationMS:     d.DurationMS,
			Timestamp:      d.Timestamp,
		}
		pr.Results = make([]CommandResultViewPrinted, 0, len(d.Results))
		for _, r := range d.Results {
			trimmed := trimLines(r.RawOutput, *limit)
			wrapped := buildWrappedLines(r.RawOutput, *wrapWidth, *limit)
			pr.Results = append(pr.Results, CommandResultViewPrinted{
				Command:        r.Command,
				RawOutput:      trimmed,
				RawOutputLines: wrapped,
				FormatOutput:   r.FormatOutput,
				Error:          r.Error,
				ExitCode:       r.ExitCode,
				DurationMS:     r.DurationMS,
			})
		}
		printed.Data = append(printed.Data, pr)
	}

	pretty, err := json.MarshalIndent(printed, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal pretty json: %v\n", err)
		os.Exit(1)
	}

	if *outFile != "" {
		if err := os.WriteFile(*outFile, pretty, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write output file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote output to %s\n", *outFile)
		return
	}
	fmt.Println(string(pretty))
}
