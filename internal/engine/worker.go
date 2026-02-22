package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"my-go-server/internal/engine/localdb"
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
	ctx = withTaskRun(ctx, taskID, runID)

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
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		msg := "初始化 Telegram 失败: " + err.Error()
		m.record(taskID, runID, 0, 0, 0, 1, msg)
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatusWithError(taskID, model.TaskStatusError, msg)
		return
	}
	api := tgRT.api
	if api == nil {
		msg := "初始化 Telegram 失败: tg api is nil"
		m.record(taskID, runID, 0, 0, 0, 1, msg)
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatusWithError(taskID, model.TaskStatusError, msg)
		return
	}

	task := t
	sourcePeer, targetPeer, sourceChannelID, err := m.SetupTaskPeers(ctx, api, &task)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		msg := "解析频道/群组失败: " + err.Error()
		m.record(taskID, runID, 0, 0, 0, 1, msg)
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatusWithError(taskID, model.TaskStatusError, msg)
		return
	}
	if task.CloneMode != 3 {
		task.EnableMediaEdit = false
	}

	kw, kwErr := loadKeywordPolicy(ctx, task)
	if kwErr != nil {
		m.record(taskID, runID, 0, 0, 0, 0, "加载关键词策略失败: "+kwErr.Error()+" (已忽略)")
	}

	localDBOpened := false
	localDBPath := ""
	if task.KeepReply {
		if _, path, err := localdb.Default.Open(taskID); err != nil {
			task.KeepReply = false
			m.record(taskID, runID, 0, 0, 0, 0, "保留回复初始化失败: 打开本地缓存库失败: "+err.Error()+" (已忽略)")
		} else {
			localDBOpened = true
			localDBPath = path
			if task.CloneMode == 1 {
				m.record(taskID, runID, 0, 0, 0, 0, "保留回复已启用: 转发模式下仅当引用消息已建立映射时才能保留回复关系")
			} else if task.HistoryOrder == model.HistoryOrderNewToOld {
				m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("保留回复已启用: localdb=%s (从新到旧模式下可能不完整)", path))
			} else {
				m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("保留回复已启用: localdb=%s", path))
			}
		}
	}

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

							SourcePeer: srcCh,
							TargetPeer: dstCh,

							SourceLinkedChatID: srcLinked.LinkedChatID,
							TargetLinkedChatID: dstLinked.LinkedChatID,

							SourceLinkedPeer: srcLinked.Peer,
							TargetLinkedPeer: dstLinked.Peer,
						}

						// Open task localdb (SQLite) for comment mirroring v2.
						db := localdb.Default.Get(taskID)
						if db == nil {
							if _, path, err := localdb.Default.Open(taskID); err != nil {
								commentCfg = nil
								m.record(taskID, runID, 0, 0, 0, 0, "评论区设置初始化失败: 打开本地缓存库失败: "+err.Error()+" (已忽略)")
							} else {
								localDBOpened = true
								commentCfg.LocalDB = localdb.Default.Get(taskID)
								m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("评论区设置已启用: source_linked=%d target_linked=%d localdb=%s", srcLinked.LinkedChatID, dstLinked.LinkedChatID, path))
							}
						} else {
							commentCfg.LocalDB = db
							if localDBPath == "" {
								localDBPath = "(opened)"
							}
							m.record(taskID, runID, 0, 0, 0, 0, fmt.Sprintf("评论区设置已启用: source_linked=%d target_linked=%d localdb=%s", srcLinked.LinkedChatID, dstLinked.LinkedChatID, localDBPath))
						}
					}
				}
			}
		}
	}
	if localDBOpened {
		defer func() {
			_ = localdb.Default.Close(taskID)
			clearMsgMappingCache(taskID)
		}()
	}

	m.record(taskID, runID, 0, 0, 0, 0, "开始克隆历史消息")
	if err := m.CloneHistoryWithPeers(ctx, api, sourcePeer, targetPeer, task, kw, runID, commentCfg); err != nil {
		if ctx.Err() != nil {
			return
		}
		msg := "历史克隆失败: " + err.Error()
		m.record(taskID, runID, 0, 0, 0, 1, msg)
		m.setStateStatus(taskID, runID, model.TaskStatusError)
		_ = updateTaskStatusWithError(taskID, model.TaskStatusError, msg)
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

		if commentCfg != nil && commentCfg.Enabled && commentCfg.LocalDB != nil {
			if err := m.registerCommentProducerTask(tgRT, commentProducerTaskConfig{
				Task:    task,
				RunID:   runID,
				Ctx:     ctx,
				Comment: commentCfg,
			}); err != nil {
				commentCfg = nil
				m.record(taskID, runID, 0, 0, 0, 0, "评论区设置初始化失败: 注册监听失败: "+err.Error()+" (已忽略)")
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

// recordDetail appends a per-task log line without broadcasting to global dashboard logs.
// It is used for verbose step logs (download/upload/watermark/md5) to avoid flooding.
func (m *TaskManager) recordDetail(taskID uint, runID uint64, logLine string) {
	if m == nil || taskID == 0 || runID == 0 {
		return
	}
	logLine = strings.TrimSpace(logLine)
	if logLine == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.states[taskID]
	if st == nil || st.RunID != runID {
		return
	}
	st.appendLogLockedNoBroadcast(logLine)
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

func updateTaskStatusWithError(taskID uint, status int, lastError string) error {
	if taskID == 0 || global.DB == nil {
		return nil
	}
	lastError = strings.TrimSpace(lastError)
	if lastError != "" {
		rs := []rune(lastError)
		if len(rs) > 255 {
			lastError = string(rs[:255])
		}
	}
	if lastError != "" && global.Logger != nil {
		global.Logger.Error("task fatal error", zap.Uint("task_id", taskID), zap.String("msg", lastError))
	}
	return global.DB.Model(&model.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{
			"status":     status,
			"last_error": lastError,
		}).Error
}
