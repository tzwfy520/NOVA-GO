package logger

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type TaskLogCleanerConfig struct {
	Enabled        bool
	RetentionDays  int
	MaxFiles       int
	MaxTotalSizeMB int64
	ScanInterval   time.Duration
	Directories    []string
}

var cleanerMu sync.Mutex
var cleanerCancel context.CancelFunc

func StartOrUpdateTaskLogCleaner(cfg TaskLogCleanerConfig) {
	cleanerMu.Lock()
	if cleanerCancel != nil {
		cleanerCancel()
		cleanerCancel = nil
	}
	cleanerMu.Unlock()

	if !cfg.Enabled {
		return
	}

	interval := cfg.ScanInterval
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	cleanerMu.Lock()
	cleanerCancel = cancel
	cleanerMu.Unlock()

	go func() {
		runOnce := func() {
			cleanupTaskLogs(cfg)
		}
		runOnce()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce()
			}
		}
	}()
}

type logFile struct {
	path    string
	modTime time.Time
	size    int64
}

func cleanupTaskLogs(cfg TaskLogCleanerConfig) {
	dirs := make([]string, 0, len(cfg.Directories))
	for _, d := range cfg.Directories {
		dd := strings.TrimSpace(d)
		if dd != "" {
			dirs = append(dirs, dd)
		}
	}
	if len(dirs) == 0 {
		dirs = []string{"./logs/collection", "./logs/backup"}
	}

	now := time.Now()
	retention := time.Duration(cfg.RetentionDays) * 24 * time.Hour

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		files := make([]logFile, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, logFile{
				path:    filepath.Join(dir, e.Name()),
				modTime: info.ModTime(),
				size:    info.Size(),
			})
		}

		if retention > 0 {
			kept := make([]logFile, 0, len(files))
			for _, f := range files {
				if now.Sub(f.modTime) > retention {
					_ = os.Remove(f.path)
					continue
				}
				kept = append(kept, f)
			}
			files = kept
		}

		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime.Before(files[j].modTime)
		})

		if cfg.MaxFiles > 0 && len(files) > cfg.MaxFiles {
			extra := len(files) - cfg.MaxFiles
			for i := 0; i < extra; i++ {
				_ = os.Remove(files[i].path)
			}
			files = files[extra:]
		}

		if cfg.MaxTotalSizeMB > 0 {
			limitBytes := cfg.MaxTotalSizeMB * 1024 * 1024
			var total int64
			for _, f := range files {
				total += f.size
			}
			for len(files) > 0 && total > limitBytes {
				_ = os.Remove(files[0].path)
				total -= files[0].size
				files = files[1:]
			}
		}
	}
}
