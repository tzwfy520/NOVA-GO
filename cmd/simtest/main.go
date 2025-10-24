package main

import (
	"context"
	"fmt"
	"time"

	sshc "github.com/sshcollectorpro/sshcollectorpro/pkg/ssh"
)

func main() {
	cfg := &sshc.Config{
		Timeout:        5 * time.Second,
		ConnectTimeout: 3 * time.Second,
		KeepAlive:      10 * time.Second,
		MaxSessions:    4,
	}
	client := sshc.NewClient(cfg)
	info := &sshc.ConnectionInfo{
		Host:     "127.0.0.1",
		Port:     22001,
		Username: "simulte-dev-huawei-01", // 设备名作为用户名
		Password: "nova",                  // 模拟器统一密码
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Connect(ctx, info); err != nil {
		panic(err)
	}
	// 1) 验证：DB优先匹配（display version）
	res1, err := client.ExecuteCommand(ctx, "display version")
	if err != nil { fmt.Println("display version error:", err) }
	fmt.Println("display version output:\n", res1.Output)
	// 2) 验证：文件回退（show run）
	res2, err := client.ExecuteCommand(ctx, "show run")
	if err != nil { fmt.Println("show run error:", err) }
	fmt.Println("show run output (head):\n", headLines(res2.Output, 10))
}

func headLines(s string, n int) string {
	if n <= 0 { return "" }
	lines := make([]string, 0, n)
	cur := ""
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
				count++
				if count >= n { break }
			}
			continue
		}
		cur += string(s[i])
	}
	if cur != "" && count < n { lines = append(lines, cur) }
	out := ""
	for _, l := range lines { out += l + "\n" }
	return out
}