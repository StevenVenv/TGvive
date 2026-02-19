package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

func (m *TaskManager) runTransferLoop(ctx context.Context, t model.Task, runID uint64) {
	taskID := t.ID
	if taskID == 0 {
		return
	}

	if global.Logger != nil {
		global.Logger.Info(
			"task engine started",
			zap.Uint("task_id", taskID),
			zap.String("source", t.SourceURL),
			zap.String("target", t.TargetURL),
		)
	}

	tgRT, err := m.ensureTelegramForTask(ctx, t)
	if err != nil {
		m.record(taskID, runID, 0, 0, 0, 1, "初始化 Telegram 失败: "+err.Error())
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatus(taskID, model.TaskStatusError)
		return
	}
	api := tgRT.api
	if api == nil {
		m.record(taskID, runID, 0, 0, 0, 1, "初始化 Telegram 失败: tg api is nil")
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatus(taskID, model.TaskStatusError)
		return
	}

	task := t
	sourcePeer, targetPeer, sourceChannelID, err := m.SetupTaskPeers(ctx, api, &task)
	if err != nil {
		m.record(taskID, runID, 0, 0, 0, 1, "解析频道/群组失败: "+err.Error())
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatus(taskID, model.TaskStatusError)
		return
	}
	if task.CloneMode != 3 {
		task.EnableMediaEdit = false
	}

	kw, kwErr := loadKeywordPolicy(ctx, task)
	if kwErr != nil {
		m.record(taskID, runID, 0, 0, 0, 0, "加载关键词策略失败: "+kwErr.Error()+" (已忽略)")
	}

	m.record(taskID, runID, 0, 0, 0, 0, "开始克隆历史消息")
	if err := m.CloneHistoryWithPeers(ctx, api, sourcePeer, targetPeer, task, kw, runID); err != nil {
		if ctx.Err() != nil {
			return
		}
		m.record(taskID, runID, 0, 0, 0, 1, "历史克隆失败: "+err.Error())
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatus(taskID, model.TaskStatusError)
		return
	}

	if ctx.Err() != nil {
		return
	}

	pollIntervalSec := 0
	enablePush := task.Realtime
	if global.DB != nil && task.StrategyID != 0 && task.UserID != 0 {
		var s model.Strategy
		if err := global.DB.Select("poll_interval", "enable_realtime", "realtime").Where("id = ? AND user_id = ?", task.StrategyID, task.UserID).First(&s).Error; err == nil {
			pollIntervalSec = s.PollInterval
			enablePush = s.EnableRealtime || s.Realtime
		}
	}
	if pollIntervalSec < 0 {
		pollIntervalSec = 0
	} else if pollIntervalSec > 0 && pollIntervalSec < 10 {
		pollIntervalSec = 10
	}

	keepAlive := enablePush || pollIntervalSec > 0
	if keepAlive {
		if global.DB != nil {
			var latest model.Task
			if err := global.DB.Select("history_cursor", "history_order", "history_max_id").Where("id = ?", taskID).First(&latest).Error; err == nil {
				task.HistoryCursor = latest.HistoryCursor
				task.HistoryOrder = latest.HistoryOrder
				task.HistoryMaxID = latest.HistoryMaxID
			}
		}

		task.Realtime = enablePush
		_ = Scheduler.RegisterTask(task)

		var commentCfg *commentPipelineConfig
		{
			st := ResolveRuntimeStrategy(task)
			_, enabled, _ := resolveCommentRule(task, st)
			if enabled {
				srcCh, okSrc := sourcePeer.(*tg.InputPeerChannel)
				dstCh, okDst := targetPeer.(*tg.InputPeerChannel)
				if !okSrc || srcCh == nil || srcCh.ChannelID == 0 {
					m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻初始化失败: 源不是频道 (已忽略)")
				} else if !okDst || dstCh == nil || dstCh.ChannelID == 0 {
					m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻初始化失败: 目标不是频道 (已忽略)")
				} else {
					srcLinked, err := tgRT.ensureLinkedChat(ctx, api, srcCh)
					if err != nil {
						m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻初始化失败: 获取源关联群失败: "+err.Error()+" (已忽略)")
					} else if srcLinked == nil || srcLinked.LinkedChatID == 0 || srcLinked.Peer == nil {
						m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻已忽略: 源频道未配置关联讨论组")
					} else {
						dstLinked, err := tgRT.ensureLinkedChat(ctx, api, dstCh)
						if err != nil {
							m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻初始化失败: 获取目标关联群失败: "+err.Error()+" (已忽略)")
						} else if dstLinked == nil || dstLinked.LinkedChatID == 0 || dstLinked.Peer == nil {
							m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻已忽略: 目标频道未配置关联讨论组")
						} else {
							commentCfg = &commentPipelineConfig{
								Enabled: true,

								SourceChannelID: sourceChannelID,
								TargetChannelID: dstCh.ChannelID,

								SourceLinkedChatID: srcLinked.LinkedChatID,
								TargetLinkedChatID: dstLinked.LinkedChatID,

								SourceLinkedPeer: srcLinked.Peer,
								TargetLinkedPeer: dstLinked.Peer,
							}

							if err := m.registerCommentTask(tgRT, commentRuntimeTaskConfig{
								Task: task,

								RunID: runID,
								Ctx:   ctx,

								SourceChannelID: sourceChannelID,
								TargetChannelID: dstCh.ChannelID,

								SourceLinkedChatID: srcLinked.LinkedChatID,
								TargetLinkedChatID: dstLinked.LinkedChatID,

								SourceLinkedPeer: srcLinked.Peer,
								TargetLinkedPeer: dstLinked.Peer,
							}); err != nil {
								commentCfg = nil
								m.record(taskID, runID, 0, 0, 0, 0, "评论区复刻初始化失败: 注册监听失败: "+err.Error()+" (已忽略)")
							} else {
								m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("评论区复刻已启用: source_linked=%d target_linked=%d", srcLinked.LinkedChatID, dstLinked.LinkedChatID))
							}
						}
					}
				}
			}
		}

		m.record(taskID, runID, 0, 0, 0, 0, "进入实时监控")
		m.markCompleted(taskID, runID, false)
		_ = m.registerRealtimeTask(tgRT, runtimeTaskConfig{
			Task:            task,
			RunID:           runID,
			Ctx:             ctx,
			SourcePeer:      sourcePeer,
			TargetPeer:      targetPeer,
			Keyword:         kw,
			PollIntervalSec: pollIntervalSec,
			Comment:         commentCfg,
		}, sourceChannelID)
		<-ctx.Done()
		m.unregisterTask(taskID, sourceChannelID)
		return
	}

	m.record(taskID, runID, 0, 0, 0, 0, "转发完成")
	m.markCompleted(taskID, runID, true)
	_ = updateTaskStatus(taskID, model.TaskStatusStopped)
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

func (m *TaskManager) setStateStatus(taskID uint, runID uint64, status int) {
	if m == nil || taskID == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.states[taskID]
	if st == nil || st.RunID != runID {
		return
	}
	st.Status = status
	if status != model.TaskStatusRunning {
		st.SpeedBaseTime = time.Time{}
		st.SpeedBaseProcessed = st.Processed
	}
}

func updateTaskStatus(taskID uint, status int) error {
	if taskID == 0 || global.DB == nil {
		return nil
	}
	return global.DB.Model(&model.Task{}).Where("id = ?", taskID).Update("status", status).Error
}
