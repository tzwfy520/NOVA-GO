package handler

import (
	"bufio"
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
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if limit <= 0 || limit > 1000 { // 安全边界
		limit = 200
	}
	q := strings.TrimSpace(c.Query("q"))
	lvl := strings.TrimSpace(c.Query("level"))

	lines, err := readAllLines(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "READ_FAILED", "message": "读取日志失败: " + err.Error()})
		return
	}

	// 过滤
	filtered := make([]string, 0, len(lines))
	for _, ln := range lines {
		if q != "" && !strings.Contains(strings.ToLower(ln), strings.ToLower(q)) {
			continue
		}
		if lvl != "" {
			// 简易级别匹配：适配 json/text 两种格式
			lc := strings.ToLower(ln)
			if !(strings.Contains(lc, "\"level\":\""+strings.ToLower(lvl)+"\"") || strings.Contains(lc, strings.ToLower(lvl))) {
				continue
			}
		}
		filtered = append(filtered, ln)
	}

	// 取尾部
	start := 0
	if len(filtered) > limit {
		start = len(filtered) - limit
	}
	tail := filtered[start:]

	c.JSON(http.StatusOK, gin.H{
		"code":    "SUCCESS",
		"message": "获取日志成功",
		"data": gin.H{
			"path":  path,
			"count": len(tail),
			"lines": tail,
		},
	})
}

func readAllLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err == nil {
		if info.Size() == 0 {
			return []string{}, nil
		}
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // up to 10MB per line
	res := make([]string, 0, 1024)
	for s.Scan() {
		res = append(res, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return res, nil
	}
	return res, nil
}