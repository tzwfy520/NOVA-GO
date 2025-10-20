package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
	"github.com/sshcollectorpro/sshcollectorpro/simulate"
)

// autoIsPortOpen tries to connect to host:port
func autoIsPortOpen(host string, port int) bool {
	if host == "" {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	// fallback hosts for wildcard listeners
	candidates := []string{"127.0.0.1", "::1", "localhost"}
	for _, h := range candidates {
		addr = net.JoinHostPort(h, strconv.Itoa(port))
		conn, err = net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// autoWaitForPortReady polls until the port is ready or timeout
func autoWaitForPortReady(host string, port int, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if autoIsPortOpen(host, port) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("port %d not ready within %ds", port, timeoutSec)
}

// autoWaitForPortClosed polls until the port is closed or timeout
func autoWaitForPortClosed(host string, port int, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if !autoIsPortOpen(host, port) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("port %d not closed within %ds", port, timeoutSec)
}

// autoKillListeningOnPort kills process(es) listening on TCP port using lsof (macOS)
func autoKillListeningOnPort(port int) ([]int, error) {
	if port <= 0 {
		return nil, nil
	}
	cmd := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t")
	out, err := cmd.Output()
	if err != nil {
		// lsof not available or no listeners
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
		_ = syscall.Kill(pid, syscall.SIGTERM)
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return pids, nil
}

// autoStartServer starts the main server via `go run`
func autoStartServer(serverMain string) (*exec.Cmd, error) {
	cmd := exec.Command("go", "run", serverMain)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// parse comma-separated ports from flag
func autoParsePortsArg(arg string) []int {
	res := []int{}
	for _, part := range strings.Split(strings.TrimSpace(arg), ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			res = append(res, v)
		}
	}
	return res
}

func uniquePorts(ports []int) []int {
	seen := make(map[int]struct{}, len(ports))
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p <= 0 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func main() {
	// Optional flags
	serverMain := flag.String("server_main", "cmd/server/main.go", "Path to server main.go to run")
	startTimeout := flag.Int("start_timeout", 10, "Seconds to wait for server port ready")
	keepRunning := flag.Bool("keep", true, "Keep server process attached and running")
	cleanupPortsArg := flag.String("cleanup_ports", "", "Comma-separated ports to clean before start; empty=clean server+simulate ports")
	flag.Parse()

	// 1) 读取配置文件
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AUTO] 读取配置失败: %v\n", err)
		os.Exit(1)
	}
	serverPort := cfg.Server.Port
	host := "127.0.0.1" // server listens on :port, connect via localhost

	// 构建默认清理端口：server.port + simulate.yaml 中的所有 namespace 端口
	cleanPorts := []int{serverPort}
	simulatePorts := make([]int, 0)
	if strings.TrimSpace(*cleanupPortsArg) == "" {
		simCfg, e := simulate.LoadConfig("simulate/simulate.yaml")
		if e == nil && simCfg != nil {
			for _, ns := range simCfg.Namespace {
				cleanPorts = append(cleanPorts, ns.Port)
				simulatePorts = append(simulatePorts, ns.Port)
			}
		} else {
			// 读取失败时使用常见默认模拟端口
			cleanPorts = append(cleanPorts, 22001, 22002)
			simulatePorts = append(simulatePorts, 22001, 22002)
		}
	} else {
		cleanPorts = autoParsePortsArg(*cleanupPortsArg)
		if len(cleanPorts) == 0 {
			cleanPorts = []int{serverPort, 22001, 22002}
		}
		// 当用户指定 cleanup_ports 时，仍尝试识别模拟端口（用于输出与等待）
		if simCfg, e := simulate.LoadConfig("simulate/simulate.yaml"); e == nil && simCfg != nil {
			for _, ns := range simCfg.Namespace {
				simulatePorts = append(simulatePorts, ns.Port)
			}
		}
		if len(simulatePorts) == 0 {
			simulatePorts = []int{22001, 22002}
		}
	}
	cleanPorts = uniquePorts(cleanPorts)
	fmt.Printf("[AUTO] 配置端口: %d\n", serverPort)
	fmt.Printf("[AUTO] 计划清理端口: %v\n", cleanPorts)

	// 2) 启动前统一清理端口并等待关闭
	for _, p := range cleanPorts {
		fmt.Printf("[AUTO] 清理占用端口: %d\n", p)
		pids, _ := autoKillListeningOnPort(p)
		if len(pids) > 0 {
			fmt.Printf("[AUTO] 已清理进程: %v\n", pids)
		} else {
			fmt.Printf("[AUTO] 未找到占用进程或 lsof 不可用 (port %d)\n", p)
		}
		_ = autoWaitForPortClosed(host, p, *startTimeout)
	}

	// 3) 尝试启动应用
	fmt.Printf("[AUTO] 尝试启动应用: go run %s\n", *serverMain)
	srvCmd, err := autoStartServer(*serverMain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AUTO] 启动失败: %v\n", err)
	}
	if err == nil {
		if e := autoWaitForPortReady(host, serverPort, *startTimeout); e == nil {
			alive := false
			if srvCmd != nil && srvCmd.Process != nil {
				if errAlive := srvCmd.Process.Signal(syscall.Signal(0)); errAlive == nil {
					alive = true
				}
			}
			if alive {
				fmt.Printf("[AUTO] 应用已监听: %s:%d\n", host, serverPort)
				// 追加：等待并输出模拟端口监听状态
				if len(simulatePorts) > 0 {
					ready := make([]string, 0, len(simulatePorts))
					for _, sp := range uniquePorts(simulatePorts) {
						_ = autoWaitForPortReady(host, sp, *startTimeout)
						if autoIsPortOpen(host, sp) {
							ready = append(ready, fmt.Sprintf("%s:%d", host, sp))
						}
					}
					if len(ready) > 0 {
						fmt.Printf("[AUTO] 模拟端口已监听: %s\n", strings.Join(ready, ", "))
					} else {
						fmt.Printf("[AUTO] 未检测到模拟端口监听 (尝试: %v)\n", simulatePorts)
					}
				}
				if *keepRunning && srvCmd != nil {
					_ = srvCmd.Wait()
				}
				return
			}
		}
	}

	// 4) 启动失败兜底：再次清理 server 端口并重试
	if autoIsPortOpen(host, serverPort) {
		fmt.Printf("[AUTO] 首次启动失败，清理端口(%d)后重试...\n", serverPort)
		_, _ = autoKillListeningOnPort(serverPort)
		_ = autoWaitForPortClosed(host, serverPort, *startTimeout)
	}

	fmt.Printf("[AUTO] 重试启动应用: go run %s\n", *serverMain)
	srvCmd2, err := autoStartServer(*serverMain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[AUTO] 重试启动失败: %v\n", err)
		os.Exit(1)
	}
	if e := autoWaitForPortReady(host, serverPort, *startTimeout); e != nil {
		fmt.Fprintf(os.Stderr, "[AUTO] 重试后端口仍未就绪: %v\n", e)
		if srvCmd2 != nil && srvCmd2.Process != nil {
			_ = srvCmd2.Process.Kill()
		}
		os.Exit(2)
	}
	fmt.Printf("[AUTO] 应用已监听: %s:%d\n", host, serverPort)
	// 追加：重试成功后同样等待并输出模拟端口监听状态
	if len(simulatePorts) > 0 {
		ready := make([]string, 0, len(simulatePorts))
		for _, sp := range uniquePorts(simulatePorts) {
			_ = autoWaitForPortReady(host, sp, *startTimeout)
			if autoIsPortOpen(host, sp) {
				ready = append(ready, fmt.Sprintf("%s:%d", host, sp))
			}
		}
		if len(ready) > 0 {
			fmt.Printf("[AUTO] 模拟端口已监听: %s\n", strings.Join(ready, ", "))
		} else {
			fmt.Printf("[AUTO] 未检测到模拟端口监听 (尝试: %v)\n", simulatePorts)
		}
	}
	if *keepRunning && srvCmd2 != nil {
		_ = srvCmd2.Wait()
	}
}
