package engine

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"go.uber.org/zap"
)

func (m *TaskManager) runTransferLoop(ctx context.Context, t model.Task, runID uint64) {
	taskID := t.ID
	if taskID == 0 {
		return
	}

	if global.Logger != nil {
		global.Logger.Info(
			"transfer loop started",
			zap.Uint("task_id", taskID),
			zap.String("source", t.SourceURL),
			zap.String("target", t.TargetURL),
		)
	}

	allowedTypes := normalizeTypeSet(t.ContentTypes.Strings())
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(taskID)))

	if global.DB != nil {
		_ = global.DB.Model(&model.Task{}).Where("id = ?", taskID).Update("status", model.TaskStatusRunning).Error
	}

	allTypes := []string{"text", "image", "video", "file", "audio"}

	for {
		if ctx.Err() != nil || !m.isActiveRun(taskID, runID) {
			return
		}

		processed, total, realtime, completed := m.getCounters(taskID, runID)

		// Initial sync finished.
		if total > 0 && processed >= total {
			if realtime {
				if !completed {
					m.record(taskID, runID, 0, 0, 0, 0, "进入实时监控")
					m.markCompleted(taskID, runID, false)
				}

				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}

				// Simulate new message arrival in realtime mode.
				if rng.Float64() < 0.35 {
					msgType := allTypes[rng.Intn(len(allTypes))]
					if len(allowedTypes) > 0 {
						if _, ok := allowedTypes[msgType]; !ok {
							m.record(taskID, runID, 1, 1, 0, 0, fmt.Sprintf("监控到新消息(%s)，但被过滤跳过", msgType))
							continue
						}
					}

					if rng.Float64() < 0.02 {
						m.record(taskID, runID, 1, 1, 0, 1, fmt.Sprintf("监控到新消息(%s)搬运失败", msgType))
						continue
					}
					m.record(taskID, runID, 1, 1, 1, 0, fmt.Sprintf("监控到新消息(%s)已搬运", msgType))
				}
				continue
			}

			m.record(taskID, runID, 0, 0, 0, 0, "搬运完成")
			m.markCompleted(taskID, runID, true)

			if global.DB != nil {
				_ = global.DB.Model(&model.Task{}).Where("id = ?", taskID).Update("status", model.TaskStatusStopped).Error
			}
			return
		}

		msgType := allTypes[rng.Intn(len(allTypes))]

		if len(allowedTypes) > 0 {
			if _, ok := allowedTypes[msgType]; !ok {
				m.record(taskID, runID, 0, 1, 0, 0, fmt.Sprintf("过滤跳过消息(%s)", msgType))
				sleepWithContext(ctx, 300*time.Millisecond)
				continue
			}
		}

		switch t.CloneMode {
		case 1:
			m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("转发消息(%s)", msgType))
		case 2:
			m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("发送消息(%s)", msgType))
		case 3:
			m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("下载并上传消息(%s)", msgType))
		default:
			m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("处理消息(%s)", msgType))
		}

		// Simulate transfer result.
		if rng.Float64() < 0.03 {
			m.record(taskID, runID, 0, 1, 0, 1, fmt.Sprintf("消息(%s)搬运失败", msgType))
		} else {
			m.record(taskID, runID, 0, 1, 1, 0, fmt.Sprintf("消息(%s)搬运成功", msgType))
		}

		sleepWithContext(ctx, 400*time.Millisecond)
	}
}

func (m *TaskManager) isActiveRun(taskID uint, runID uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st := m.states[taskID]
	if st == nil || st.RunID != runID {
		return false
	}
	_, ok := m.cancelers[taskID]
	return ok
}

func (m *TaskManager) getCounters(taskID uint, runID uint64) (processed int, total int, realtime bool, completed bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st := m.states[taskID]
	if st == nil || st.RunID != runID {
		return 0, 0, false, false
	}
	return st.Processed, st.Total, st.Realtime, st.Completed
}

// record updates counters and appends a log line.
// totalDelta is used by realtime mode when new messages arrive.
func (m *TaskManager) record(taskID uint, runID uint64, totalDelta int, processedDelta int, successDelta int, failDelta int, logLine string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.states[taskID]
	if st == nil || st.RunID != runID {
		return
	}

	if totalDelta != 0 {
		st.Total += totalDelta
		if st.Total < 0 {
			st.Total = 0
		}
	}
	if processedDelta != 0 {
		st.Processed += processedDelta
		if st.Processed < 0 {
			st.Processed = 0
		}
	}
	if successDelta != 0 {
		st.Success += successDelta
		if st.Success < 0 {
			st.Success = 0
		}
	}
	if failDelta != 0 {
		st.Fail += failDelta
		if st.Fail < 0 {
			st.Fail = 0
		}
	}
	if logLine != "" {
		st.appendLogLocked(logLine)
	}
}

func (m *TaskManager) markCompleted(taskID uint, runID uint64, stopRun bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.states[taskID]
	if st == nil || st.RunID != runID {
		return
	}

	st.Completed = true
	if stopRun {
		if cancel, ok := m.cancelers[taskID]; ok {
			cancel()
			delete(m.cancelers, taskID)
		}
		st.Status = model.TaskStatusStopped
		st.SpeedBaseTime = time.Time{}
		st.SpeedBaseProcessed = st.Processed
	}
}

func normalizeTypeSet(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
