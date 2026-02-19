package engine

import (
	"context"
	"strings"
	"time"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

const (
	commentSendDelayMin = 300 * time.Millisecond
	commentSendDelayMax = 1 * time.Second
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

func (m *TaskManager) cloneCommentsForTrunk(ctx context.Context, api *tg.Client, task model.Task, sourcePeer tg.InputPeerClass, targetPeer tg.InputPeerClass, commentCfg *commentPipelineConfig, sourceChannelMsgID int, targetChannelMsgID int) {
	if err := ctx.Err(); err != nil {
		return
	}
	if m == nil || api == nil {
		return
	}
	if commentCfg == nil || !commentCfg.Enabled {
		return
	}
	if commentCfg.SourceLinkedPeer == nil || commentCfg.TargetLinkedPeer == nil {
		return
	}
	if sourcePeer == nil || targetPeer == nil {
		return
	}
	if sourceChannelMsgID <= 0 || targetChannelMsgID <= 0 {
		return
	}

	srcRoot, dstRoot, werr := m.writeMessageMappingWithRoots(ctx, api, commentCfg, sourcePeer, targetPeer, sourceChannelMsgID, targetChannelMsgID)
	if werr != nil && global.Logger != nil {
		global.Logger.Warn(
			"write message mapping failed",
			zap.Uint("task_id", task.ID),
			zap.Int("source_msg_id", sourceChannelMsgID),
			zap.Int("target_msg_id", targetChannelMsgID),
			zap.Error(werr),
		)
	}
	if srcRoot <= 0 || dstRoot <= 0 {
		return
	}

	comments, err := fetchRepliesByRoot(ctx, api, commentCfg.SourceLinkedPeer, srcRoot, commentFetchPageSize, commentFetchMaxTotal)
	if err != nil {
		if global.Logger != nil {
			global.Logger.Warn(
				"fetch replies failed",
				zap.Uint("task_id", task.ID),
				zap.Int("source_root_id", srcRoot),
				zap.Error(err),
			)
		}
		return
	}
	if len(comments) == 0 {
		return
	}
	if len(comments) >= commentFetchMaxTotal && global.Logger != nil {
		global.Logger.Warn(
			"comments truncated by max_total",
			zap.Uint("task_id", task.ID),
			zap.Int("source_root_id", srcRoot),
			zap.Int("max_total", commentFetchMaxTotal),
		)
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
		if !shouldCloneCommentByIdentity(msg, commentCfg.SourceChannelID, rule.FilterMode, trustedSet, rule.AllowAnonymous) {
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

		if err := m.sendCommentWithFallback(ctx, api, commentCfg.SourceLinkedPeer, commentCfg.TargetLinkedPeer, msg, dstRoot, task.ID); err != nil {
			if d, ok := tgerr.AsFloodWait(err); ok {
				if global.Logger != nil {
					global.Logger.Warn(
						"send mirrored comment floodwait, skipping remaining comments for trunk",
						zap.Uint("task_id", task.ID),
						zap.Int("comment_msg_id", msg.ID),
						zap.Duration("wait", d),
						zap.Error(err),
					)
				}
				break
			}
			if global.Logger != nil {
				global.Logger.Warn(
					"send mirrored comment failed",
					zap.Uint("task_id", task.ID),
					zap.Int("comment_msg_id", msg.ID),
					zap.Error(err),
				)
			}
		}

		sleepRandom(ctx, commentSendDelayMin, commentSendDelayMax)
	}
}
