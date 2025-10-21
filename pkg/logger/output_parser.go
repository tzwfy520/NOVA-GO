package logger

import (
	"strings"
	
	"github.com/sirupsen/logrus"
)

// OutputLines 表示命令输出的头部和尾部行
type OutputLines struct {
	HeadLines []string `json:"head_lines"`
	TailLines []string `json:"tail_lines"`
}

// ParseOutputLines 解析命令输出，提取头部和尾部行
// maxLines: 最多提取的行数（head和tail各自的最大行数）
func ParseOutputLines(output string, maxLines int) OutputLines {
	if maxLines <= 0 {
		maxLines = 5 // 默认最多5行
	}

	// 统一换行符处理
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")
	
	// 按行分割
	lines := strings.Split(output, "\n")
	
	// 过滤空行（可选，根据需求决定是否保留空行）
	var filteredLines []string
	for _, line := range lines {
		// 保留所有行，包括空行，以保持原始输出格式
		filteredLines = append(filteredLines, line)
	}
	
	totalLines := len(filteredLines)
	
	var headLines, tailLines []string
	
	if totalLines == 0 {
		return OutputLines{
			HeadLines: headLines,
			TailLines: tailLines,
		}
	}
	
	// 提取头部行
	headCount := maxLines
	if headCount > totalLines {
		headCount = totalLines
	}
	headLines = make([]string, headCount)
	copy(headLines, filteredLines[:headCount])
	
	// 提取尾部行
	tailCount := maxLines
	if tailCount > totalLines {
		tailCount = totalLines
	}
	
	// 如果总行数小于等于maxLines，head和tail是相同的
	if totalLines <= maxLines {
		tailLines = make([]string, len(headLines))
		copy(tailLines, headLines)
	} else {
		// 从末尾开始提取
		startIdx := totalLines - tailCount
		tailLines = make([]string, tailCount)
		copy(tailLines, filteredLines[startIdx:])
	}
	
	return OutputLines{
		HeadLines: headLines,
		TailLines: tailLines,
	}
}

// FormatOutputLines 格式化输出行为字符串，用于日志记录
func FormatOutputLines(lines OutputLines) string {
	var parts []string
	
	if len(lines.HeadLines) > 0 {
		parts = append(parts, "head-lines: ["+strings.Join(lines.HeadLines, " ⟩ ")+"]")
	}
	
	if len(lines.TailLines) > 0 {
		// 如果head和tail完全相同，只显示一次
		if !areSlicesEqual(lines.HeadLines, lines.TailLines) {
			parts = append(parts, "tail-lines: ["+strings.Join(lines.TailLines, " ⟩ ")+"]")
		}
	}
	
	return strings.Join(parts, ", ")
}

// areSlicesEqual 比较两个字符串切片是否相等
func areSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DebugCommandOutput 在debug级别记录命令输出的head/tail-lines
func DebugCommandOutput(command string, output string, maxLines int) {
	if GetLogger().Level < logrus.DebugLevel {
		return // 如果不是debug级别，直接返回
	}
	
	lines := ParseOutputLines(output, maxLines)
	if len(lines.HeadLines) == 0 && len(lines.TailLines) == 0 {
		return // 没有输出内容，不记录
	}
	
	formattedLines := FormatOutputLines(lines)
	Debugf("Command echo [%s]: %s", command, formattedLines)
}