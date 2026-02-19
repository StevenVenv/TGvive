package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
	"gorm.io/gorm/clause"
)

func lowerKeywordList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, w := range in {
		k := strings.ToLower(strings.TrimSpace(w))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// StoreMappingForTrunk stores discussion-root mapping into task local SQLite DB.
func (m *TaskManager) StoreMappingForTrunk(
	ctx context.Context,
	api *tg.Client,
	task model.Task,
	cfg *commentPipelineConfig,
	sourceChannelPeer tg.InputPeerClass,
	targetChannelPeer tg.InputPeerClass,
	sourceChannelMsgID int,
	targetChannelMsgID int,
) (sourceRootID int, targetRootID int, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if m == nil || api == nil {
		return 0, 0, errors.New("engine not initialized")
	}
	if cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return 0, 0, nil
	}
	if sourceChannelPeer == nil || targetChannelPeer == nil {
		return 0, 0, errors.New("tg peer is nil")
	}
	if sourceChannelMsgID <= 0 || targetChannelMsgID <= 0 {
		return 0, 0, nil
	}
	if cfg.SourceLinkedChatID == 0 || cfg.TargetLinkedChatID == 0 {
		return 0, 0, nil
	}

	// Only channels have linked discussions.
	_, okSrc := sourceChannelPeer.(*tg.InputPeerChannel)
	_, okDst := targetChannelPeer.(*tg.InputPeerChannel)
	if !okSrc || !okDst {
		return 0, 0, nil
	}

	srcRes, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  sourceChannelPeer,
		MsgID: sourceChannelMsgID,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("get source discussion message: %w", err)
	}
	sourceRootID = findDiscussionRootMsgID(srcRes, cfg.SourceLinkedChatID)
	if sourceRootID <= 0 {
		return 0, 0, nil
	}

	dstRes, err := api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  targetChannelPeer,
		MsgID: targetChannelMsgID,
	})
	if err != nil {
		return sourceRootID, 0, fmt.Errorf("get target discussion message: %w", err)
	}
	targetRootID = findDiscussionRootMsgID(dstRes, cfg.TargetLinkedChatID)
	if targetRootID <= 0 {
		return sourceRootID, 0, nil
	}

	rec := localdb.LocalMapping{
		SourceRootID: sourceRootID,
		TargetRootID: targetRootID,
	}

	if err := cfg.LocalDB.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source_root_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"target_root_id",
				"updated_at",
			}),
		}).
		Create(&rec).Error; err != nil {
		if global.Logger != nil {
			global.Logger.Warn("store local mapping failed", zap.Uint("task_id", task.ID), zap.Error(err))
		}
		// Best-effort: still return roots.
		return sourceRootID, targetRootID, nil
	}

	return sourceRootID, targetRootID, nil
}

// ProduceHistoryCommentsForTrunk fetches historical replies for a trunk post and stores them into local SQLite.
// It does not send comments directly (sending is handled by Consumer).
func (m *TaskManager) ProduceHistoryCommentsForTrunk(
	ctx context.Context,
	api *tg.Client,
	task model.Task,
	cfg *commentPipelineConfig,
	sourceChannelPeer tg.InputPeerClass,
	targetChannelPeer tg.InputPeerClass,
	sourceChannelMsgID int,
	targetChannelMsgID int,
) {
	if err := ctx.Err(); err != nil {
		return
	}
	if m == nil || api == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return
	}
	if cfg.SourceLinkedPeer == nil {
		return
	}

	srcRoot, _, err := m.StoreMappingForTrunk(ctx, api, task, cfg, sourceChannelPeer, targetChannelPeer, sourceChannelMsgID, targetChannelMsgID)
	if err != nil || srcRoot <= 0 {
		return
	}

	comments, err := fetchRepliesByRoot(ctx, api, cfg.SourceLinkedPeer, srcRoot, commentFetchPageSize, commentFetchMaxTotal)
	if err != nil {
		if global.Logger != nil {
			global.Logger.Warn("fetch replies failed", zap.Uint("task_id", task.ID), zap.Int("source_root_id", srcRoot), zap.Error(err))
		}
		return
	}
	if len(comments) == 0 {
		return
	}

	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveCommentRule(task, st)
	if !enabled {
		return
	}

	trustedSet := make(map[int64]struct{}, len(rule.TrustedUserIDs))
	for _, id := range rule.TrustedUserIDs {
		if id > 0 {
			trustedSet[id] = struct{}{}
		}
	}

	typeList := normalizeRuntimeTypeList(rule.AllowedTypes)
	allowedSet := map[string]struct{}(nil)
	if len(typeList) > 0 {
		allowedSet = normalizeTypeSet(typeList)
	}
	blockLower := lowerKeywordList(rule.BlockKeywords)

	now := time.Now()

	for _, msg := range comments {
		if err := ctx.Err(); err != nil {
			return
		}
		if msg == nil || msg.ID <= 0 {
			continue
		}
		if extractCommentRootMsgID(msg) != srcRoot {
			continue
		}
		if !shouldCloneCommentByIdentity(msg, cfg.SourceChannelID, rule.FilterMode, trustedSet, rule.AllowAnonymous) {
			continue
		}
		if allowedSet != nil {
			ct := m.DetectContentType(msg)
			if _, ok := allowedSet[ct]; !ok {
				continue
			}
		}
		if hitBlockKeywords(msg.Message, blockLower) {
			continue
		}

		payload, err := localdb.WashMessage(msg, m.DetectContentType)
		if err != nil {
			if global.Logger != nil {
				global.Logger.Warn("wash comment failed", zap.Uint("task_id", task.ID), zap.Int("msg_id", msg.ID), zap.Error(err))
			}
			continue
		}
		if payload == nil {
			continue
		}
		b, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		rec := localdb.LocalComment{
			MsgID:         msg.ID,
			ReplyToRootID: srcRoot,
			Status:        "pending",
			Attempts:      0,
			NextAttemptAt: now,
			LightPayload:  b,
		}

		if err := cfg.LocalDB.
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "msg_id"}},
				DoNothing: true,
			}).
			Create(&rec).Error; err != nil && global.Logger != nil {
			global.Logger.Warn("store local comment failed", zap.Uint("task_id", task.ID), zap.Int("msg_id", msg.ID), zap.Error(err))
		}
	}
}

func (m *TaskManager) StoreRealtimeComment(ctx context.Context, task model.Task, cfg *commentPipelineConfig, msg *tg.Message) {
	if err := ctx.Err(); err != nil {
		return
	}
	if m == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return
	}
	if msg == nil || msg.ID <= 0 {
		return
	}

	rootID := extractCommentRootMsgID(msg)
	if rootID <= 0 {
		return
	}

	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveCommentRule(task, st)
	if !enabled {
		return
	}

	trustedSet := make(map[int64]struct{}, len(rule.TrustedUserIDs))
	for _, id := range rule.TrustedUserIDs {
		if id > 0 {
			trustedSet[id] = struct{}{}
		}
	}

	typeList := normalizeRuntimeTypeList(rule.AllowedTypes)
	allowedSet := map[string]struct{}(nil)
	if len(typeList) > 0 {
		allowedSet = normalizeTypeSet(typeList)
	}
	blockLower := lowerKeywordList(rule.BlockKeywords)

	if !shouldCloneCommentByIdentity(msg, cfg.SourceChannelID, rule.FilterMode, trustedSet, rule.AllowAnonymous) {
		return
	}
	if allowedSet != nil {
		ct := m.DetectContentType(msg)
		if _, ok := allowedSet[ct]; !ok {
			return
		}
	}
	if hitBlockKeywords(msg.Message, blockLower) {
		return
	}

	payload, err := localdb.WashMessage(msg, m.DetectContentType)
	if err != nil || payload == nil {
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	rec := localdb.LocalComment{
		MsgID:         msg.ID,
		ReplyToRootID: rootID,
		Status:        "pending",
		Attempts:      0,
		NextAttemptAt: time.Now(),
		LightPayload:  b,
	}
	if err := cfg.LocalDB.
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "msg_id"}},
			DoNothing: true,
		}).
		Create(&rec).Error; err != nil && global.Logger != nil {
		global.Logger.Warn("store local comment failed", zap.Uint("task_id", task.ID), zap.Int("msg_id", msg.ID), zap.Error(err))
	}
}
