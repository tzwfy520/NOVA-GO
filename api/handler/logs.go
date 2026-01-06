package handler

import (
	"bytes"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/sshcollectorpro/sshcollectorpro/internal/config"
)

// LogsHandler 日志查询处理器
type LogsHandler struct{}

func NewLogsHandler() *LogsHandler { return &LogsHandler{} }

// TailLogs 简易日志Tail查询（按关键字、级别过滤，返回末尾N行）
func (h *LogsHandler) TailLogs(c *gin.Context) {
	cfg := config.Get()
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CONFIG_MISSING", "message": "配置未初始化"})
		return
	}
	path := strings.TrimSpace(cfg.Log.FilePath)
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "LOG_PATH_EMPTY", "message": "日志路径未配置"})
		return
	}
	limitStr := c.DefaultQuery("limit", "")
	if strings.TrimSpace(limitStr) == "" {
		limitStr = c.DefaultQuery("lines", "200")
	}
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 1000 { // 安全边界
		limit = 200
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		q = strings.TrimSpace(c.Query("keyword"))
	}
	lvl := strings.TrimSpace(c.Query("level"))

	lines, err := tailLinesFiltered(path, limit, q, lvl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "READ_FAILED", "message": "读取日志失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "获取日志成功",
		"data": gin.H{
			"path":  path,
			"count": len(lines),
			"lines": lines,
		},
	})
}

func tailLinesFiltered(filePath string, limit int, q string, lvl string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if size <= 0 || limit <= 0 {
		return []string{}, nil
	}

	qLower := strings.ToLower(strings.TrimSpace(q))
	lvlLower := strings.ToLower(strings.TrimSpace(lvl))

	matches := make([]string, 0, limit)
	var carry []byte

	const chunkSize int64 = 64 * 1024
	var offset int64 = size

	for offset > 0 && len(matches) < limit {
		readSize := chunkSize
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize

		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, offset); err != nil {
			return nil, err
		}

		if len(carry) > 0 {
			buf = append(buf, carry...)
			carry = nil
		}

		parts := bytes.Split(buf, []byte{'\n'})
		if offset > 0 && len(parts) > 0 {
			carry = parts[0]
			parts = parts[1:]
		}

		for i := len(parts) - 1; i >= 0 && len(matches) < limit; i-- {
			if len(parts[i]) == 0 {
				continue
			}
			ln := string(parts[i])
			if qLower != "" && !strings.Contains(strings.ToLower(ln), qLower) {
				continue
			}
			if lvlLower != "" {
				lc := strings.ToLower(ln)
				if !(strings.Contains(lc, "\"level\":\""+lvlLower+"\"") || strings.Contains(lc, lvlLower)) {
					continue
				}
			}
			matches = append(matches, ln)
		}
	}

	if offset == 0 && len(carry) > 0 && len(matches) < limit {
		ln := string(carry)
		if strings.TrimSpace(ln) != "" {
			ok := true
			if qLower != "" && !strings.Contains(strings.ToLower(ln), qLower) {
				ok = false
			}
			if ok && lvlLower != "" {
				lc := strings.ToLower(ln)
				if !(strings.Contains(lc, "\"level\":\""+lvlLower+"\"") || strings.Contains(lc, lvlLower)) {
					ok = false
				}
			}
			if ok {
				matches = append(matches, ln)
			}
		}
	}

	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}

	return matches, nil
}
