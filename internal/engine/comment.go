package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	groupAnonymousBotID int64 = 1087968824
	muteForeverUnix     int   = 2147483647
)

type linkedChatInfo struct {
	LinkedChatID int64
	Peer         *tg.InputPeerChannel
}

type commentPipelineConfig struct {
	Enabled bool

	SourceChannelID int64
	TargetChannelID int64

	SourceLinkedChatID int64
	TargetLinkedChatID int64

	SourceLinkedPeer *tg.InputPeerChannel
	TargetLinkedPeer *tg.InputPeerChannel
}

type commentRuntimeTaskConfig struct {
	Task model.Task

	RunID uint64
	Ctx   context.Context

	SourceChannelID int64
	TargetChannelID int64

	SourceLinkedChatID int64
	TargetLinkedChatID int64

	SourceLinkedPeer *tg.InputPeerChannel
	TargetLinkedPeer *tg.InputPeerChannel
}

type commentRuntimeTask struct {
	Task model.Task

	RunID uint64
	Ctx   context.Context

	SourceChannelID int64
	TargetChannelID int64

	SourceLinkedChatID int64
	TargetLinkedChatID int64

	SourceLinkedPeer *tg.InputPeerChannel
	TargetLinkedPeer *tg.InputPeerChannel

	mu          sync.Mutex
	ruleKey     string
	ruleEnabled bool
	filterMode  string

	allowAnonymous  bool
	trustedUserSet  map[int64]struct{}
	allowedTypes    map[string]struct{}
	allowedTypesKey string
	blockKeywords   []string // lower-case

	queue    chan *tg.Message
	done     chan struct{}
	stopOnce sync.Once
}

func newCommentRuntimeTask(cfg commentRuntimeTaskConfig) *commentRuntimeTask {
	t := &commentRuntimeTask{
		Task: cfg.Task,

		RunID: cfg.RunID,
		Ctx:   cfg.Ctx,

		SourceChannelID: cfg.SourceChannelID,
		TargetChannelID: cfg.TargetChannelID,

		SourceLinkedChatID: cfg.SourceLinkedChatID,
		TargetLinkedChatID: cfg.TargetLinkedChatID,

		SourceLinkedPeer: cfg.SourceLinkedPeer,
		TargetLinkedPeer: cfg.TargetLinkedPeer,

		queue: make(chan *tg.Message, 512),
		done:  make(chan struct{}),
	}

	t.refreshRuleSnapshot()
	return t
}

func (t *commentRuntimeTask) stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() {
		close(t.done)
	})
}

func (t *commentRuntimeTask) enqueue(msg *tg.Message) bool {
	if t == nil || msg == nil {
		return false
	}
	if t.Ctx == nil || t.Ctx.Err() != nil {
		return false
	}
	select {
	case <-t.done:
		return false
	default:
	}
	select {
	case t.queue <- msg:
		return true
	default:
		// drop on overload
		return false
	}
}

func (t *commentRuntimeTask) refreshRuleSnapshot() {
	if t == nil {
		return
	}

	st := ResolveRuntimeStrategy(t.Task)
	rule, enabled, key := resolveCommentRule(t.Task, st)

	t.mu.Lock()
	defer t.mu.Unlock()

	if key == t.ruleKey {
		return
	}
	t.ruleKey = key
	t.ruleEnabled = enabled
	t.filterMode = rule.FilterMode
	t.allowAnonymous = rule.AllowAnonymous

	// Trusted users set.
	if len(rule.TrustedUserIDs) > 0 {
		set := make(map[int64]struct{}, len(rule.TrustedUserIDs))
		for _, id := range rule.TrustedUserIDs {
			if id > 0 {
				set[id] = struct{}{}
			}
		}
		t.trustedUserSet = set
	} else {
		t.trustedUserSet = nil
	}

	// Allowed types set.
	typeList := normalizeRuntimeTypeList(rule.AllowedTypes)
	typesKey := "all"
	if len(typeList) > 0 {
		typesKey = strings.Join(typeList, ",")
	}
	if typesKey != t.allowedTypesKey {
		t.allowedTypesKey = typesKey
		if len(typeList) == 0 {
			t.allowedTypes = nil
		} else {
			t.allowedTypes = normalizeTypeSet(typeList)
		}
	}

	// Block keywords (lower-case).
	if len(rule.BlockKeywords) > 0 {
		seen := make(map[string]struct{}, len(rule.BlockKeywords))
		out := make([]string, 0, len(rule.BlockKeywords))
		for _, w := range rule.BlockKeywords {
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
		t.blockKeywords = out
	} else {
		t.blockKeywords = nil
	}
}

func (t *commentRuntimeTask) run(m *TaskManager, api *tg.Client) {
	if t == nil || m == nil || api == nil || t.Ctx == nil {
		return
	}

	for {
		select {
		case <-t.done:
			return
		case <-t.Ctx.Done():
			return
		case msg, ok := <-t.queue:
			if !ok {
				return
			}
			if msg == nil || msg.ID <= 0 {
				continue
			}
			if err := t.Ctx.Err(); err != nil {
				return
			}

			// Refresh rule best-effort to follow strategy updates.
			t.refreshRuleSnapshot()

			t.mu.Lock()
			enabled := t.ruleEnabled
			filterMode := t.filterMode
			allowAnonymous := t.allowAnonymous
			trusted := t.trustedUserSet
			allowedTypes := t.allowedTypes
			blockKeywords := t.blockKeywords
			t.mu.Unlock()

			if !enabled {
				continue
			}

			rootID := extractCommentRootMsgID(msg)
			if rootID <= 0 {
				continue
			}

			var mapping model.MessageMapping
			if global.DB == nil {
				continue
			}
			err := global.DB.
				Where("source_channel_id = ? AND source_msg_id = ? AND target_channel_id = ?", t.SourceChannelID, rootID, t.TargetChannelID).
				First(&mapping).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				if global.Logger != nil {
					global.Logger.Warn("query message mapping failed", zap.Uint("task_id", t.Task.ID), zap.Error(err))
				}
				continue
			}
			if mapping.TargetMsgID <= 0 {
				continue
			}

			if !shouldCloneCommentByIdentity(msg, t.SourceChannelID, filterMode, trusted, allowAnonymous) {
				continue
			}

			// Type filter.
			if allowedTypes != nil {
				ct := m.DetectContentType(msg)
				if _, ok := allowedTypes[ct]; !ok {
					continue
				}
			}

			// Keyword blacklist.
			if hitBlockKeywords(msg.Message, blockKeywords) {
				continue
			}

			if err := m.sendCommentWithFallback(t.Ctx, api, t.SourceLinkedPeer, t.TargetLinkedPeer, msg, mapping.TargetMsgID, t.Task.ID); err != nil {
				if global.Logger != nil {
					global.Logger.Warn(
						"send mirrored comment failed",
						zap.Uint("task_id", t.Task.ID),
						zap.Int("msg_id", msg.ID),
						zap.Error(err),
					)
				}
			}
		}
	}
}

func extractCommentRootMsgID(msg *tg.Message) int {
	if msg == nil || msg.ReplyTo == nil {
		return 0
	}
	h, ok := msg.ReplyTo.(*tg.MessageReplyHeader)
	if !ok || h == nil {
		return 0
	}
	if top, ok := h.GetReplyToTopID(); ok && top > 0 {
		return top
	}
	if mid, ok := h.GetReplyToMsgID(); ok && mid > 0 {
		return mid
	}
	return 0
}

func hitBlockKeywords(text string, blacklistLower []string) bool {
	if len(blacklistLower) == 0 {
		return false
	}
	lt := strings.ToLower(text)
	if strings.TrimSpace(lt) == "" {
		return false
	}
	for _, w := range blacklistLower {
		if w == "" {
			continue
		}
		if strings.Contains(lt, w) {
			return true
		}
	}
	return false
}

func shouldCloneCommentByIdentity(msg *tg.Message, sourceChannelID int64, filterMode string, trusted map[int64]struct{}, allowAnonymous bool) bool {
	filterMode = strings.ToLower(strings.TrimSpace(filterMode))
	if filterMode == "all" {
		return true
	}
	// Alias.
	if filterMode == "whitelist" {
		filterMode = "owner_only"
	}

	if filterMode != "owner_only" {
		filterMode = "owner_only"
	}

	from, ok := msg.GetFromID()
	if !ok || from == nil {
		return false
	}

	// Owner (send-as-channel in linked discussion).
	if ch, ok := from.(*tg.PeerChannel); ok && ch != nil && ch.ChannelID == sourceChannelID {
		return true
	}

	if u, ok := from.(*tg.PeerUser); ok && u != nil {
		if allowAnonymous && u.UserID == groupAnonymousBotID {
			return true
		}
		if trusted != nil {
			if _, ok := trusted[u.UserID]; ok {
				return true
			}
		}
	}

	return false
}

// ShouldCloneComment is the public identity predicate (used by tests and other modules).
func ShouldCloneComment(msg *tg.Message, sourceChannelID int64, rule model.CommentRule) bool {
	trusted := make(map[int64]struct{}, len(rule.TrustedUserIDs))
	for _, id := range rule.TrustedUserIDs {
		if id > 0 {
			trusted[id] = struct{}{}
		}
	}
	return shouldCloneCommentByIdentity(msg, sourceChannelID, rule.FilterMode, trusted, rule.AllowAnonymous)
}

func defaultRuntimeCommentRule() model.CommentRule {
	return model.CommentRule{
		Enable:         true,
		FilterMode:     "owner_only",
		AllowAnonymous: false,
		AllowedTypes:   []string{"text", "file"},
	}
}

func resolveCommentRule(task model.Task, st *model.Strategy) (rule model.CommentRule, enabled bool, key string) {
	// 1) strategy.comment_rule
	if st != nil && len(st.CommentRule) > 0 && strings.TrimSpace(string(st.CommentRule)) != "null" {
		var r model.CommentRule
		if err := json.Unmarshal(st.CommentRule, &r); err == nil {
			r = normalizeRuntimeCommentRule(r)
			b, _ := json.Marshal(r)
			return r, r.Enable, string(b)
		}
	}

	// 2) legacy clone_comment fallback
	if task.CloneComment || (st != nil && st.CloneComment) {
		r := normalizeRuntimeCommentRule(defaultRuntimeCommentRule())
		b, _ := json.Marshal(r)
		return r, true, string(b)
	}

	return model.CommentRule{}, false, "off"
}

func normalizeRuntimeCommentRule(in model.CommentRule) model.CommentRule {
	out := in
	out.FilterMode = strings.ToLower(strings.TrimSpace(out.FilterMode))
	switch out.FilterMode {
	case "whitelist":
		out.FilterMode = "owner_only"
	case "owner_only", "all":
	default:
		out.FilterMode = "owner_only"
	}

	// Normalize IDs.
	if len(out.TrustedUserIDs) > 0 {
		seen := make(map[int64]struct{}, len(out.TrustedUserIDs))
		list := make([]int64, 0, len(out.TrustedUserIDs))
		for _, id := range out.TrustedUserIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			list = append(list, id)
		}
		out.TrustedUserIDs = list
	} else {
		out.TrustedUserIDs = nil
	}

	// Allowed types normalization (engine aliases).
	out.AllowedTypes = normalizeRuntimeTypeList(out.AllowedTypes)
	if len(out.AllowedTypes) == 0 {
		out.AllowedTypes = nil
	}

	// Block keywords normalization.
	if len(out.BlockKeywords) > 0 {
		seen := make(map[string]struct{}, len(out.BlockKeywords))
		list := make([]string, 0, len(out.BlockKeywords))
		for _, w := range out.BlockKeywords {
			k := strings.TrimSpace(w)
			if k == "" {
				continue
			}
			lk := strings.ToLower(k)
			if _, ok := seen[lk]; ok {
				continue
			}
			seen[lk] = struct{}{}
			list = append(list, k)
		}
		out.BlockKeywords = list
	} else {
		out.BlockKeywords = nil
	}

	return out
}

func (rt *telegramRuntime) ensureLinkedChat(ctx context.Context, api *tg.Client, channelPeer *tg.InputPeerChannel) (*linkedChatInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, errors.New("telegram runtime is nil")
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if channelPeer == nil || channelPeer.ChannelID == 0 {
		return nil, errors.New("channel peer is nil")
	}

	chID := channelPeer.ChannelID

	rt.linkedMu.Lock()
	if rt.linkedChats == nil {
		rt.linkedChats = make(map[int64]*linkedChatInfo)
	}
	if cached, ok := rt.linkedChats[chID]; ok {
		rt.linkedMu.Unlock()
		if cached == nil || cached.LinkedChatID == 0 || cached.Peer == nil {
			return nil, nil
		}
		return cached, nil
	}
	rt.linkedMu.Unlock()

	full, err := api.ChannelsGetFullChannel(ctx, &tg.InputChannel{
		ChannelID:  channelPeer.ChannelID,
		AccessHash: channelPeer.AccessHash,
	})
	if err != nil {
		return nil, err
	}

	var linkedID int64
	if fc, ok := full.FullChat.(*tg.ChannelFull); ok && fc != nil {
		if v, ok := fc.GetLinkedChatID(); ok && v != 0 {
			linkedID = v
		}
	}
	if linkedID == 0 {
		rt.linkedMu.Lock()
		rt.linkedChats[chID] = &linkedChatInfo{LinkedChatID: 0}
		rt.linkedMu.Unlock()
		return nil, nil
	}

	accessHash := int64(0)
	for _, c := range full.Chats {
		ch, ok := c.(*tg.Channel)
		if !ok || ch == nil || ch.ID != linkedID {
			continue
		}
		if ah, ok := ch.GetAccessHash(); ok && ah != 0 {
			accessHash = ah
			break
		}
		if ch.AccessHash != 0 {
			accessHash = ch.AccessHash
			break
		}
	}
	if accessHash == 0 {
		return nil, fmt.Errorf("linked chat access hash missing (linked_chat_id=%d)", linkedID)
	}

	linkedPeer := &tg.InputPeerChannel{ChannelID: linkedID, AccessHash: accessHash}

	// Ensure joined.
	if _, err := api.ChannelsJoinChannel(ctx, &tg.InputChannel{ChannelID: linkedID, AccessHash: accessHash}); err != nil {
		if !tgerr.Is(err, "USER_ALREADY_PARTICIPANT") {
			return nil, err
		}
	}

	// Mute forever.
	settings := tg.InputPeerNotifySettings{}
	settings.SetMuteUntil(muteForeverUnix)
	if _, err := api.AccountUpdateNotifySettings(ctx, &tg.AccountUpdateNotifySettingsRequest{
		Peer:     &tg.InputNotifyPeer{Peer: linkedPeer},
		Settings: settings,
	}); err != nil {
		return nil, err
	}

	info := &linkedChatInfo{LinkedChatID: linkedID, Peer: linkedPeer}
	rt.linkedMu.Lock()
	if rt.linkedChats == nil {
		rt.linkedChats = make(map[int64]*linkedChatInfo)
	}
	rt.linkedChats[chID] = info
	rt.linkedMu.Unlock()

	return info, nil
}

func (m *TaskManager) registerCommentTask(tgRT *telegramRuntime, cfg commentRuntimeTaskConfig) error {
	if m == nil || tgRT == nil {
		return errors.New("telegram runtime not initialized")
	}
	if cfg.Task.ID == 0 {
		return errors.New("task id is required")
	}
	if cfg.SourceChannelID == 0 {
		return errors.New("source_channel_id is required")
	}
	if cfg.TargetChannelID == 0 {
		return errors.New("target_channel_id is required")
	}
	if cfg.SourceLinkedChatID == 0 || cfg.TargetLinkedChatID == 0 {
		return errors.New("linked_chat_id is required")
	}
	if cfg.SourceLinkedPeer == nil || cfg.TargetLinkedPeer == nil {
		return errors.New("linked peer is nil")
	}
	if cfg.Ctx == nil {
		return errors.New("task context is nil")
	}

	taskID := cfg.Task.ID

	var toStop *commentRuntimeTask
	tgRT.tasksMu.Lock()
	if prev := tgRT.commentTasksByID[taskID]; prev != nil {
		toStop = prev
		prev.stop()
	}
	taskPtr := newCommentRuntimeTask(cfg)
	if tgRT.commentTasksByID == nil {
		tgRT.commentTasksByID = make(map[uint]*commentRuntimeTask)
	}
	tgRT.commentTasksByID[taskID] = taskPtr

	mm := tgRT.commentByLinkedChat[cfg.SourceLinkedChatID]
	if mm == nil {
		if tgRT.commentByLinkedChat == nil {
			tgRT.commentByLinkedChat = make(map[int64]map[uint]*commentRuntimeTask)
		}
		mm = make(map[uint]*commentRuntimeTask)
		tgRT.commentByLinkedChat[cfg.SourceLinkedChatID] = mm
	}
	mm[taskID] = taskPtr
	api := tgRT.api
	tgRT.tasksMu.Unlock()

	_ = toStop

	if api == nil {
		return errors.New("tg api is nil")
	}

	go taskPtr.run(m, api)
	return nil
}

func (m *TaskManager) dispatchCommentMessage(tgRT *telegramRuntime, channelID int64, msg *tg.Message) {
	if m == nil || tgRT == nil || channelID == 0 || msg == nil {
		return
	}

	tgRT.tasksMu.RLock()
	mm := tgRT.commentByLinkedChat[channelID]
	if len(mm) == 0 {
		tgRT.tasksMu.RUnlock()
		return
	}
	tasks := make([]*commentRuntimeTask, 0, len(mm))
	for _, rt := range mm {
		if rt != nil {
			tasks = append(tasks, rt)
		}
	}
	tgRT.tasksMu.RUnlock()

	for _, rt := range tasks {
		if rt == nil || rt.Ctx == nil || rt.Ctx.Err() != nil {
			continue
		}
		_ = rt.enqueue(msg)
	}
}

func (m *TaskManager) sendCommentWithFallback(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msg *tg.Message, replyToMsgID int, taskID uint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("tg api is nil")
	}
	if peer == nil {
		return errors.New("tg peer is nil")
	}
	if msg == nil {
		return nil
	}
	if replyToMsgID <= 0 {
		return errors.New("reply_to_msg_id is required")
	}

	replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID}

	// Text-only.
	if msg.Media == nil {
		if strings.TrimSpace(msg.Message) == "" {
			return nil
		}
		rid, err := randomID()
		if err != nil {
			return err
		}
		req := &tg.MessagesSendMessageRequest{
			Peer:     peer,
			ReplyTo:  replyTo,
			Message:  msg.Message,
			RandomID: rid,
		}
		if len(msg.Entities) > 0 {
			req.Entities = msg.Entities
		}
		_, err = api.MessagesSendMessage(ctx, req)
		return err
	}

	// Try send-by-reference (CloneMode=2 behavior).
	inputMedia, err := convertMessageMediaToInput(msg.Media)
	if err != nil {
		if errors.Is(err, ErrUnsupportedMedia) {
			// Fallback to caption-only.
			if strings.TrimSpace(msg.Message) == "" {
				return nil
			}
			rid, rerr := randomID()
			if rerr != nil {
				return rerr
			}
			req := &tg.MessagesSendMessageRequest{
				Peer:     peer,
				ReplyTo:  replyTo,
				Message:  msg.Message,
				RandomID: rid,
			}
			if len(msg.Entities) > 0 {
				req.Entities = msg.Entities
			}
			_, err = api.MessagesSendMessage(ctx, req)
			return err
		}
		return err
	}

	rid, err := randomID()
	if err != nil {
		return err
	}
	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Media:    inputMedia,
		Message:  msg.Message,
		RandomID: rid,
	}
	if len(msg.Entities) > 0 {
		req.Entities = msg.Entities
	}
	if _, err := api.MessagesSendMedia(ctx, req); err == nil {
		return nil
	} else if !isForwardOrCopyRestricted(err) {
		return err
	}

	// Upload fallback (CloneMode=3 behavior, without processors/MD5 changes).
	localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, api, sourcePeer, msg, taskID)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer func() { _ = cleanup() }()
	}

	inputFile, err := m.UploadFile(ctx, api, localPath)
	if err != nil {
		return err
	}

	uploaded, err := m.WrapUploadedMedia(ctx, api, inputFile, msg)
	if err != nil {
		return err
	}
	if uploaded == nil {
		return ErrUnsupportedMedia
	}

	rid2, err := randomID()
	if err != nil {
		return err
	}
	req2 := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Media:    uploaded,
		Message:  msg.Message,
		RandomID: rid2,
	}
	if len(msg.Entities) > 0 {
		req2.Entities = msg.Entities
	}
	_, err = api.MessagesSendMedia(ctx, req2)
	return err
}
