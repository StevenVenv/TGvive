package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
) (sourceRootID int, targetRootID int, sourceHasReplies bool, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, false, err
	}
	if m == nil || api == nil {
		return 0, 0, false, errors.New("engine not initialized")
	}
	if cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return 0, 0, false, nil
	}
	if sourceChannelPeer == nil || targetChannelPeer == nil {
		return 0, 0, false, errors.New("tg peer is nil")
	}
	if sourceChannelMsgID <= 0 || targetChannelMsgID <= 0 {
		return 0, 0, false, nil
	}
	if cfg.SourceLinkedChatID == 0 || cfg.TargetLinkedChatID == 0 {
		return 0, 0, false, nil
	}

	// Only channels have linked discussions.
	_, okSrc := sourceChannelPeer.(*tg.InputPeerChannel)
	_, okDst := targetChannelPeer.(*tg.InputPeerChannel)
	if !okSrc || !okDst {
		return 0, 0, false, nil
	}

	sourceRootID, sourceHasReplies, err = getDiscussionRootMetaWithRetry(ctx, api, sourceChannelPeer, sourceChannelMsgID, cfg.SourceLinkedChatID)
	if err != nil {
		return 0, 0, false, fmt.Errorf("get source discussion message: %w", err)
	}
	if sourceRootID <= 0 {
		return 0, 0, sourceHasReplies, nil
	}

	targetRootID, err = getDiscussionRootIDWithRetry(ctx, api, targetChannelPeer, targetChannelMsgID, cfg.TargetLinkedChatID)
	if err != nil {
		return sourceRootID, 0, sourceHasReplies, fmt.Errorf("get target discussion message: %w", err)
	}
	if targetRootID <= 0 {
		return sourceRootID, 0, sourceHasReplies, nil
	}

	rec := localdb.RootMapping{
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
			global.Logger.Warn("store root mapping failed", zap.Uint("task_id", task.ID), zap.Error(err))
		}
		return sourceRootID, targetRootID, sourceHasReplies, err
	}

	// Repair: some comment updates may reference the source channel post msg_id (not discussion root).
	// Rewrite pending rows to discussion root once we know the mapping.
	if sourceChannelMsgID > 0 && sourceRootID > 0 && sourceChannelMsgID != sourceRootID {
		// Avoid accidental collisions: if sourceChannelMsgID is already a mapped root, keep it unchanged.
		if !hasRootMapping(cfg.LocalDB, sourceChannelMsgID) {
			_ = cfg.LocalDB.Model(&localdb.CommentQueue{}).
				Where("reply_to_root_id = ? AND status = ?", sourceChannelMsgID, localdb.CommentStatusPending).
				Update("reply_to_root_id", sourceRootID).Error
		}
	}

	return sourceRootID, targetRootID, sourceHasReplies, nil
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
) (sourceRootID int, targetRootID int) {
	if err := ctx.Err(); err != nil {
		return 0, 0
	}
	if m == nil || api == nil || cfg == nil || !cfg.Enabled || cfg.LocalDB == nil {
		return 0, 0
	}
	if cfg.SourceLinkedPeer == nil {
		return 0, 0
	}

	srcRoot, dstRoot, sourceHasReplies, err := m.StoreMappingForTrunk(ctx, api, task, cfg, sourceChannelPeer, targetChannelPeer, sourceChannelMsgID, targetChannelMsgID)
	if err != nil || srcRoot <= 0 || dstRoot <= 0 {
		if err != nil && global.Logger != nil {
			global.Logger.Warn("store trunk mapping failed", zap.Uint("task_id", task.ID), zap.Int("source_msg_id", sourceChannelMsgID), zap.Error(err))
		}
		return srcRoot, dstRoot
	}

	// No existing replies/comments: skip backfill to avoid transient MSG_ID_INVALID for newly created discussion roots.
	if !sourceHasReplies {
		return srcRoot, dstRoot
	}

	comments, err := fetchRepliesByRoot(ctx, api, cfg.SourceLinkedPeer, srcRoot, commentFetchPageSize, commentFetchMaxTotal)
	if err != nil {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("评论抓取失败: source_root=%d err=%v", srcRoot, err))
		if global.Logger != nil {
			global.Logger.Warn("fetch replies failed", zap.Uint("task_id", task.ID), zap.Int("source_root_id", srcRoot), zap.Error(err))
		}
		return srcRoot, dstRoot
	}
	if len(comments) == 0 {
		return srcRoot, dstRoot
	}

	st := ResolveRuntimeStrategy(task)
	rule, enabled, _ := resolveCommentRule(task, st)
	if !enabled {
		return srcRoot, dstRoot
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

	queued := 0
	skipped := 0
	for _, msg := range comments {
		if err := ctx.Err(); err != nil {
			return srcRoot, dstRoot
		}
		if msg == nil || msg.ID <= 0 {
			skipped++
			continue
		}
		if extractCommentRootMsgID(msg) != srcRoot {
			skipped++
			continue
		}
		if !shouldCloneCommentByIdentity(msg, cfg.SourceChannelID, rule.FilterMode, trustedSet, rule.AllowAnonymous) {
			skipped++
			continue
		}
		if allowedSet != nil {
			ct := m.DetectContentType(msg)
			if _, ok := allowedSet[ct]; !ok {
				skipped++
				continue
			}
		}

		msgToQueue := msg
		if msg.GroupedID == 0 {
			if cfg.Keyword != nil {
				out, skip := applyKeywordPolicyToMessage(msg, cfg.Keyword)
				if skip {
					skipped++
					continue
				}
				msgToQueue = out
			}

			if hitBlockKeywords(msgToQueue.Message, blockLower) {
				skipped++
				continue
			}
		}

		payload, err := localdb.WashMessage(msgToQueue, m.DetectContentType)
		if err != nil {
			if global.Logger != nil {
				global.Logger.Warn("wash comment failed", zap.Uint("task_id", task.ID), zap.Int("msg_id", msg.ID), zap.Error(err))
			}
			skipped++
			continue
		}
		if payload == nil {
			skipped++
			continue
		}
		payload.ReplyToMsgID = extractCommentReplyToMsgIDInLinkedChat(msgToQueue, srcRoot, cfg)
		b, err := json.Marshal(payload)
		if err != nil {
			skipped++
			continue
		}

		rec := localdb.CommentQueue{
			MsgID:         msg.ID,
			ReplyToRootID: srcRoot,
			GroupedID:     msg.GroupedID,
			Status:        localdb.CommentStatusPending,
			TargetMsgID:   0,
			Payload:       b,
		}

		res := cfg.LocalDB.
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "msg_id"}},
				DoNothing: true,
			}).
			Create(&rec)
		if err := res.Error; err != nil {
			skipped++
			if global.Logger != nil {
				global.Logger.Warn("store local comment failed", zap.Uint("task_id", task.ID), zap.Int("msg_id", msg.ID), zap.Error(err))
			}
			continue
		}
		if res.RowsAffected > 0 {
			queued++
		} else {
			skipped++
		}
	}

	// Only log when there are actual replies (avoid spamming empty roots).
	if len(comments) > 0 {
		recordTaskDetailFromCtx(ctx, fmt.Sprintf("评论抓取: source_root=%d fetched=%d queued=%d skipped=%d", srcRoot, len(comments), queued, skipped))
	}

	return srcRoot, dstRoot
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

	rec := localdb.CommentQueue{
		MsgID:         msg.ID,
		ReplyToRootID: rootID,
		GroupedID:     msg.GroupedID,
		Status:        localdb.CommentStatusPending,
		TargetMsgID:   0,
		Payload:       b,
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
