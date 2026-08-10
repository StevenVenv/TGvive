package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var taskDetailLogMu sync.Mutex

func appendTaskDetailLog(taskID uint, runID uint64, msg string) {
	if taskID == 0 || runID == 0 {
		return
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}

	line := fmt.Sprintf("[%s] [run:%d] %s\n", time.Now().Format("2006-01-02 15:04:05"), runID, msg)
	path := filepath.Join("logs", fmt.Sprintf("task_%d_detail.log", taskID))

	taskDetailLogMu.Lock()
	defer taskDetailLogMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line)
}
