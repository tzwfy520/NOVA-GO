package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

// Self-test for simulate SSH service: restart server then run SSH tests.
// It will:
// 1) Read ports from configs/config.yaml and simulate/simulate.yaml.
// 2) Kill processes occupying these ports.
// 3) Start the main server.
// 4) Verify simulate namespaces via SSH for Cisco/Huawei device users.
// 5) Print PASS/FAIL summary and exit with code accordingly.

func main() {
	fmt.Println("[SELFTEST] Start")

	serverPort := readServerPort()
	defaultPort := readNamespacePort("default")
	if defaultPort == 0 {
		// fallback: pick the first parsed port or known 22001
		ports := readSimulateNamespacePorts()
		if len(ports) > 0 { defaultPort = ports[0] } else { defaultPort = 22001 }
	}
	fmt.Printf("[SELFTEST] serverPort=%d defaultPort=%d\n", serverPort, defaultPort)

	// Kill any existing processes listening on these ports
	killPorts([]int{defaultPort, serverPort})

	// Build and start the server
	cmd := exec.Command("go", "run", "cmd/server/main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Printf("[SELFTEST] Failed to start server: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[SELFTEST] Server started (pid=%d)\n", cmd.Process.Pid)

	// Wait ports ready
	ok := waitPortsReady([]int{defaultPort, serverPort}, 12*time.Second)
	if !ok {
		fmt.Println("[SELFTEST] Ports not ready in time")
		_ = cmd.Process.Signal(os.Interrupt)
		os.Exit(1)
	}

	// Run SSH tests on default namespace
	allPass := true
	if !testCisco(defaultPort) { allPass = false }
	if !testHuawei(defaultPort) { allPass = false }

	// Stop server gracefully
	_ = cmd.Process.Signal(os.Interrupt)
	// Allow graceful shutdown
	time.Sleep(1500 * time.Millisecond)

	if allPass {
		fmt.Println("[SELFTEST] PASS: all simulate tests succeeded")
		os.Exit(0)
	} else {
		fmt.Println("[SELFTEST] FAIL: some simulate tests failed")
		os.Exit(2)
	}
}

func readServerPort() int {
	v := viper.New()
	v.SetConfigFile("configs/config.yaml")
	if err := v.ReadInConfig(); err != nil { return 18000 }
	return v.GetInt("server.port")
}

func readSimulateNamespacePorts() []int {
	v := viper.New()
	v.SetConfigFile("simulate/simulate.yaml")
	if err := v.ReadInConfig(); err != nil { return nil }
	ns := v.GetStringMap("namespace")
	res := make([]int, 0, len(ns))
	for name := range ns {
		key := fmt.Sprintf("namespace.%s.port", name)
		p := v.GetInt(key)
		if p > 0 { res = append(res, p) }
	}
	return res
}

func readNamespacePort(nsName string) int {
	v := viper.New()
	v.SetConfigFile("simulate/simulate.yaml")
	if err := v.ReadInConfig(); err != nil { return 0 }
	return v.GetInt(fmt.Sprintf("namespace.%s.port", nsName))
}

func killPorts(ports []int) {
	for _, p := range ports {
		sh := fmt.Sprintf("PIDS=$(lsof -ti tcp:%d); if [ -n \"$PIDS\" ]; then kill -9 $PIDS; fi", p)
		_ = exec.Command("bash", "-lc", sh).Run()
		fmt.Printf("[SELFTEST] Ensure port %d free\n", p)
	}
}

func waitPortsReady(ports []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		all := true
		for _, p := range ports {
			if !isPortOpen(p) { all = false; break }
		}
		if all { return true }
		if time.Now().After(deadline) { return false }
		time.Sleep(250 * time.Millisecond)
	}
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 800*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

func newSSHClient(port int, user, pass string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         6 * time.Second,
	}
	cfg.Auth = []ssh.AuthMethod{
		ssh.Password(pass),
		ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions { answers[i] = pass }
			return answers, nil
		}),
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return ssh.Dial("tcp", addr, cfg)
}

func testCisco(port int) bool {
	fmt.Println("[SELFTEST] Cisco: connect")
	cli, err := newSSHClient(port, "cisco-01", "nova")
	if err != nil { fmt.Println("[SELFTEST] Cisco connect failed:", err); return false }
	defer cli.Close()

	sess, err := cli.NewSession()
	if err != nil { fmt.Println("[SELFTEST] Cisco new session failed:", err); return false }
	defer sess.Close()

	modes := ssh.TerminalModes{ ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400 }
	if err := sess.RequestPty("xterm", 80, 24, modes); err != nil { fmt.Println("[SELFTEST] Cisco pty failed:", err); return false }

	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	if err := sess.Shell(); err != nil { fmt.Println("[SELFTEST] Cisco shell failed:", err); return false }

	reader := bufio.NewReader(stdout)
	prompt, _ := readLineWithTimeout(reader, 4*time.Second)
	if !strings.Contains(prompt, "cisco-01>") { fmt.Println("[SELFTEST] Cisco prompt mismatch:", prompt); return false }

	// terminal length 0
	stdin.Write([]byte("terminal length 0\r\n"))
	out1 := readUntilPrompt(reader, "cisco-01>", 5*time.Second)
	if !strings.Contains(out1, "Set terminal length") { fmt.Println("[SELFTEST] Cisco tl0 missing"); return false }

	// show version
	stdin.Write([]byte("show version\r\n"))
	out2 := readUntilPrompt(reader, "cisco-01>", 5*time.Second)
	if !strings.Contains(out2, "Cisco IOS Software") { fmt.Println("[SELFTEST] Cisco show version missing"); return false }

	// enable -> password nova -> prompt '#'
	stdin.Write([]byte("enable\r\n"))
	pwPrompt, _ := readLineWithTimeout(reader, 3*time.Second)
	if !strings.Contains(strings.ToLower(pwPrompt), "password") { fmt.Println("[SELFTEST] Cisco enable no password prompt:", pwPrompt); return false }
	stdin.Write([]byte("nova\r\n"))
	prompt2, _ := readLineWithTimeout(reader, 3*time.Second)
	if !strings.Contains(prompt2, "cisco-01#") { fmt.Println("[SELFTEST] Cisco enable prompt mismatch:", prompt2); return false }

	// unknown command
	stdin.Write([]byte("foobar\r\n"))
	out3 := readUntilPrompt(reader, "cisco-01#", 4*time.Second)
	if !strings.Contains(out3, "unsupportted command") { fmt.Println("[SELFTEST] Cisco unsupported missing"); return false }

	fmt.Println("[SELFTEST] Cisco: PASS")
	return true
}

func testHuawei(port int) bool {
	fmt.Println("[SELFTEST] Huawei: connect")
	cli, err := newSSHClient(port, "huawei-01", "nova")
	if err != nil { fmt.Println("[SELFTEST] Huawei connect failed:", err); return false }
	defer cli.Close()

	sess, err := cli.NewSession()
	if err != nil { fmt.Println("[SELFTEST] Huawei new session failed:", err); return false }
	defer sess.Close()

	modes := ssh.TerminalModes{ ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400 }
	if err := sess.RequestPty("xterm", 80, 24, modes); err != nil { fmt.Println("[SELFTEST] Huawei pty failed:", err); return false }

	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	if err := sess.Shell(); err != nil { fmt.Println("[SELFTEST] Huawei shell failed:", err); return false }

	reader := bufio.NewReader(stdout)
	prompt, _ := readLineWithTimeout(reader, 4*time.Second)
	if !strings.Contains(prompt, "huawei-01>") { fmt.Println("[SELFTEST] Huawei prompt mismatch:", prompt); return false }

	stdin.Write([]byte("screen-length disable\r\n"))
	out1 := readUntilPrompt(reader, "huawei-01>", 5*time.Second)
	if !(strings.Contains(out1, "screen length is 0") || strings.Contains(out1, "Info: The screen length is 0")) {
		fmt.Println("[SELFTEST] Huawei screen-length missing")
		return false
	}

	stdin.Write([]byte("display version\r\n"))
	out2 := readUntilPrompt(reader, "huawei-01>", 5*time.Second)
	if !strings.Contains(out2, "Huawei Versatile Routing Platform") { fmt.Println("[SELFTEST] Huawei display version missing"); return false }

	stdin.Write([]byte("system-view\r\n"))
	out3 := readUntilPrompt(reader, "huawei-01>", 5*time.Second)
	if !strings.Contains(out3, "Enter system view") { fmt.Println("[SELFTEST] Huawei system-view missing"); return false }

	stdin.Write([]byte("save\r\n"))
	out4 := readUntilPrompt(reader, "huawei-01>", 5*time.Second)
	if !strings.Contains(out4, "configuration successfully") { fmt.Println("[SELFTEST] Huawei save missing"); return false }

	fmt.Println("[SELFTEST] Huawei: PASS")
	return true
}

func readLineWithTimeout(r *bufio.Reader, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) { return "", fmt.Errorf("timeout") }
		line, err := r.ReadString('\n')
		if err == nil { return cleanLine(line), nil }
	}
}

func readUntilPrompt(r *bufio.Reader, prompt string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	var b strings.Builder
	for {
		if time.Now().After(deadline) { break }
		line, err := r.ReadString('\n')
		if err == nil {
			clean := cleanLine(line)
			b.WriteString(clean)
			b.WriteString("\n")
			if strings.Contains(clean, prompt) { break }
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return b.String()
}

func cleanLine(s string) string {
	// normalize CRLF -> \n then trim
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n")
}