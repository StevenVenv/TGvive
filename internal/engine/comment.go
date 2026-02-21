package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	groupAnonymousBotID int64 = 1087968824
	muteForeverUnix     int   = 2147483647
)

const commentAlbumFlushDebounce = 800 * time.Millisecond

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

	// LocalDB is the task-scoped SQLite handle used by Producer/Consumer.
	// It is nil when comment mirroring is disabled or localdb init failed.
	LocalDB *gorm.DB

	// sendMu prevents concurrent flushes (trunk mapping flush vs realtime comment flush).
	sendMu sync.Mutex
}

type commentProducerTaskConfig struct {
	Task model.Task

	RunID uint64
	Ctx   context.Context

	Comment *commentPipelineConfig
}

type commentProducerTask struct {
	Task model.Task

	RunID uint64
	Ctx   context.Context

	comment *commentPipelineConfig

	mu          sync.Mutex
	ruleKey     string
	ruleEnabled bool
	filterMode  string

	allowAnonymous  bool
	trustedUserSet  map[int64]struct{}
	allowedTypes    map[string]struct{}
	allowedTypesKey string
	blockKeywords   []string // lower-case

	queue    chan commentEvent
	done     chan struct{}
	stopOnce sync.Once
}

type commentEventKind uint8

const (
	commentEventNew commentEventKind = iota
	commentEventEdit
)

type commentEvent struct {
	kind commentEventKind
	msg  *tg.Message
}

func newCommentProducerTask(cfg commentProducerTaskConfig) *commentProducerTask {
	t := &commentProducerTask{
		Task: cfg.Task,

		RunID: cfg.RunID,
		Ctx:   cfg.Ctx,

		comment: cfg.Comment,

		queue: make(chan commentEvent, 512),
		done:  make(chan struct{}),
	}

	t.refreshRuleSnapshot()
	return t
}

func (t *commentProducerTask) stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() {
		close(t.done)
	})
}

func (t *commentProducerTask) enqueue(kind commentEventKind, msg *tg.Message) bool {
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
	case t.queue <- commentEvent{kind: kind, msg: msg}:
		return true
	default:
		// drop on overload
		return false
	}
}

func (t *commentProducerTask) refreshRuleSnapshot() {
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

func (t *commentProducerTask) run(m *TaskManager, api *tg.Client) {
	if t == nil || m == nil || api == nil || t.Ctx == nil {
		return
	}
	if t.comment == nil || t.comment.LocalDB == nil {
		return
	}

	loadTargetRootID := func(sourceRootID int) int {
		if sourceRootID <= 0 {
			return 0
		}
		var mapping localdb.RootMapping
		if err := t.comment.LocalDB.Where("source_root_id = ?", sourceRootID).First(&mapping).Error; err != nil || mapping.TargetRootID <= 0 {
			return 0
		}
		return mapping.TargetRootID
	}

	flushRoot := func(sourceRootID int) {
		targetRootID := loadTargetRootID(sourceRootID)
		if targetRootID <= 0 {
			return
		}
		m.sendPendingCommentsForRoot(t.Ctx, api, t.Task, t.comment, sourceRootID, targetRootID)
	}

	var (
		pendingGroupedID int64
		pendingRootID    int

		flushTimer *time.Timer
		flushC     <-chan time.Time
	)

	stopFlushTimer := func() {
		if flushTimer == nil {
			return
		}
		if !flushTimer.Stop() {
			select {
			case <-flushTimer.C:
			default:
			}
		}
		flushTimer = nil
		flushC = nil
	}

	scheduleGroupedFlush := func() {
		if pendingGroupedID == 0 || pendingRootID <= 0 {
			stopFlushTimer()
			return
		}
		if flushTimer == nil {
			flushTimer = time.NewTimer(commentAlbumFlushDebounce)
			flushC = flushTimer.C
			return
		}
		if !flushTimer.Stop() {
			select {
			case <-flushTimer.C:
			default:
			}
		}
		flushTimer.Reset(commentAlbumFlushDebounce)
		flushC = flushTimer.C
	}

	flushPendingGrouped := func() {
		if pendingGroupedID == 0 || pendingRootID <= 0 {
			stopFlushTimer()
			return
		}
		defer func() {
			pendingGroupedID = 0
			pendingRootID = 0
			stopFlushTimer()
		}()

		flushRoot(pendingRootID)
	}
	defer stopFlushTimer()

	for {
		select {
		case <-t.done:
			return
		case <-t.Ctx.Done():
			return
		case <-flushC:
			flushPendingGrouped()
		case ev, ok := <-t.queue:
			if !ok {
				return
			}
			msg := ev.msg
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

			switch ev.kind {
			case commentEventNew:
				rootID := extractCommentRootMsgID(msg)
				if rootID <= 0 {
					continue
				}

				// If a previous grouped (album) comment is pending and the new message is not part of it,
				// flush the previous group first to preserve ordering.
				if pendingGroupedID != 0 && pendingRootID > 0 {
					if msg.GroupedID == 0 || msg.GroupedID != pendingGroupedID || rootID != pendingRootID {
						flushPendingGrouped()
					}
				}

				if !shouldCloneCommentByIdentity(msg, t.comment.SourceChannelID, filterMode, trusted, allowAnonymous) {
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

				payload, err := localdb.WashMessage(msg, m.DetectContentType)
				if err != nil || payload == nil {
					continue
				}
				b, err := json.Marshal(payload)
				if err != nil {
					continue
				}

				rec := localdb.CommentQueue{
					MsgID:         msg.ID,
					ReplyToRootID: rootID,
					GroupedID:     msg.GroupedID,
					Status:        localdb.CommentStatusPending,
					TargetMsgID:   0,
					Payload:       b,
				}
				if err := t.comment.LocalDB.
					Clauses(clause.OnConflict{
						Columns:   []clause.Column{{Name: "msg_id"}},
						DoNothing: true,
					}).
					Create(&rec).Error; err != nil && global.Logger != nil {
					global.Logger.Warn("store comment queue failed", zap.Uint("task_id", t.Task.ID), zap.Int("msg_id", msg.ID), zap.Error(err))
					continue
				}

				// For grouped media (album), delay flush slightly to collect the whole group.
				if msg.GroupedID != 0 {
					pendingGroupedID = msg.GroupedID
					pendingRootID = rootID
					scheduleGroupedFlush()
					continue
				}

				flushRoot(rootID)
			case commentEventEdit:
				// Edit only matters for comments (replies in linked chat).
				if extractCommentRootMsgID(msg) <= 0 {
					continue
				}

				payload, err := localdb.WashMessage(msg, m.DetectContentType)
				if err != nil || payload == nil {
					continue
				}
				b, err := json.Marshal(payload)
				if err != nil {
					continue
				}

				// Branch A: pending (update payload only, no network send).
				res := t.comment.LocalDB.Model(&localdb.CommentQueue{}).
					Where("msg_id = ? AND status = ?", msg.ID, localdb.CommentStatusPending).
					Update("payload", b)
				if res.Error == nil && res.RowsAffected == 1 {
					continue
				}

				// Branch B: success (async edit to target linked group).
				var rec localdb.CommentQueue
				if err := t.comment.LocalDB.Select("status", "target_msg_id").
					First(&rec, "msg_id = ?", msg.ID).Error; err != nil {
					continue
				}
				_ = t.comment.LocalDB.Model(&localdb.CommentQueue{}).
					Where("msg_id = ?", msg.ID).
					Update("payload", b).Error
				if rec.Status == localdb.CommentStatusSuccess && rec.TargetMsgID > 0 {
					m.submitCommentEdit(t.Ctx, api, t.Task.ID, t.comment, msg.ID, rec.TargetMsgID)
				}
			default:
				continue
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

	if filterMode != "owner_only" && filterMode != "owner_or_linked" {
		filterMode = "owner_only"
	}
	if filterMode == "owner_or_linked" {
		allowAnonymous = true
	}

	from, ok := msg.GetFromID()
	if !ok || from == nil {
		// Some channel/supergroup messages may omit from_id when posted by the channel itself.
		// Treat them as "send as group/channel" identity based on peer_id.
		if peerChID, ok := peerToChannelID(msg.PeerID); ok && peerChID != 0 {
			from = &tg.PeerChannel{ChannelID: peerChID}
		} else {
			return false
		}
	}

	// Owner (send-as-channel in linked discussion).
	if ch, ok := from.(*tg.PeerChannel); ok && ch != nil {
		// 1) Channel itself.
		if ch.ChannelID == sourceChannelID {
			return true
		}
		// 2) Discussion group itself (anonymous admin "send as group").
		if peerChID, ok := peerToChannelID(msg.PeerID); ok && peerChID != 0 && ch.ChannelID == peerChID {
			return true
		}
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
	case "owner_only", "owner_or_linked", "all":
	default:
		out.FilterMode = "owner_only"
	}

	// In linked mode, always allow "send as group" identity.
	if out.FilterMode == "owner_or_linked" {
		out.AllowAnonymous = true
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

func (m *TaskManager) registerCommentProducerTask(tgRT *telegramRuntime, cfg commentProducerTaskConfig) error {
	if m == nil || tgRT == nil {
		return errors.New("telegram runtime not initialized")
	}
	if cfg.Task.ID == 0 {
		return errors.New("task id is required")
	}
	if cfg.Comment == nil || !cfg.Comment.Enabled {
		return errors.New("comment config is required")
	}
	if cfg.Comment.SourceChannelID == 0 {
		return errors.New("source_channel_id is required")
	}
	if cfg.Comment.SourceLinkedChatID == 0 {
		return errors.New("source_linked_chat_id is required")
	}
	if cfg.Comment.SourceLinkedPeer == nil {
		return errors.New("source linked peer is nil")
	}
	if cfg.Comment.LocalDB == nil {
		return errors.New("localdb is nil")
	}
	if cfg.Ctx == nil {
		return errors.New("task context is nil")
	}

	taskID := cfg.Task.ID

	var toStop *commentProducerTask
	tgRT.tasksMu.Lock()
	if prev := tgRT.commentTasksByID[taskID]; prev != nil {
		toStop = prev
		prev.stop()
	}
	taskPtr := newCommentProducerTask(cfg)
	if tgRT.commentTasksByID == nil {
		tgRT.commentTasksByID = make(map[uint]*commentProducerTask)
	}
	tgRT.commentTasksByID[taskID] = taskPtr

	mm := tgRT.commentByLinkedChat[cfg.Comment.SourceLinkedChatID]
	if mm == nil {
		if tgRT.commentByLinkedChat == nil {
			tgRT.commentByLinkedChat = make(map[int64]map[uint]*commentProducerTask)
		}
		mm = make(map[uint]*commentProducerTask)
		tgRT.commentByLinkedChat[cfg.Comment.SourceLinkedChatID] = mm
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

func (m *TaskManager) dispatchCommentNew(tgRT *telegramRuntime, channelID int64, msg *tg.Message) {
	m.dispatchCommentEvent(tgRT, channelID, commentEventNew, msg)
}

func (m *TaskManager) dispatchCommentEdit(tgRT *telegramRuntime, channelID int64, msg *tg.Message) {
	m.dispatchCommentEvent(tgRT, channelID, commentEventEdit, msg)
}

func (m *TaskManager) dispatchCommentEvent(tgRT *telegramRuntime, channelID int64, kind commentEventKind, msg *tg.Message) {
	if m == nil || tgRT == nil || channelID == 0 || msg == nil {
		return
	}

	tgRT.tasksMu.RLock()
	mm := tgRT.commentByLinkedChat[channelID]
	if len(mm) == 0 {
		tgRT.tasksMu.RUnlock()
		return
	}
	tasks := make([]*commentProducerTask, 0, len(mm))
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
		_ = rt.enqueue(kind, msg)
	}
}

// dispatchCommentMessage is kept for backward compatibility (treated as "new").
func (m *TaskManager) dispatchCommentMessage(tgRT *telegramRuntime, channelID int64, msg *tg.Message) {
	m.dispatchCommentNew(tgRT, channelID, msg)
}

func (m *TaskManager) sendCommentWithFallback(ctx context.Context, api *tg.Client, sourcePeer tg.InputPeerClass, peer tg.InputPeerClass, msg *tg.Message, replyToMsgID int, task model.Task) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, errors.New("tg api is nil")
	}
	if peer == nil {
		return nil, errors.New("tg peer is nil")
	}
	if msg == nil {
		return nil, nil
	}
	if replyToMsgID <= 0 {
		return nil, errors.New("reply_to_msg_id is required")
	}

	replyTo := &tg.InputReplyToMessage{ReplyToMsgID: replyToMsgID}

	// Text-only.
	if msg.Media == nil {
		if strings.TrimSpace(msg.Message) == "" {
			return nil, nil
		}
		rid, err := randomID()
		if err != nil {
			return nil, err
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
		upd, err := api.MessagesSendMessage(ctx, req)
		return extractSentMsgIDs(upd), err
	}

	// Try send-by-reference (CloneMode=2 behavior).
	inputMedia, err := convertMessageMediaToInput(msg.Media)
	if err != nil {
		if errors.Is(err, ErrUnsupportedMedia) {
			// Fallback to caption-only.
			if strings.TrimSpace(msg.Message) == "" {
				return nil, nil
			}
			rid, rerr := randomID()
			if rerr != nil {
				return nil, rerr
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
			upd, err := api.MessagesSendMessage(ctx, req)
			return extractSentMsgIDs(upd), err
		}
		return nil, err
	}

	caption := msg.Message
	entities := msg.Entities
	if out, truncated := sanitizeMediaCaptionText(caption); truncated {
		caption = out
		entities = nil
	}

	rid, err := randomID()
	if err != nil {
		return nil, err
	}
	req := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Media:    inputMedia,
		Message:  caption,
		RandomID: rid,
	}
	if len(entities) > 0 {
		req.Entities = entities
	}
	if upd, err := api.MessagesSendMedia(ctx, req); err == nil {
		return extractSentMsgIDs(upd), nil
	} else if !isForwardOrCopyRestricted(err) {
		return nil, err
	}

	// Upload fallback (CloneMode=3 behavior, without processors/MD5 changes).
	localPath, _, cleanup, err := m.DownloadFileWithPeer(ctx, api, sourcePeer, msg, task.ID)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer func() { _ = cleanup() }()
	}

	inputFile, err := m.UploadFile(ctx, api, localPath)
	if err != nil {
		return nil, err
	}

	uploaded, err := m.WrapUploadedMedia(ctx, api, inputFile, msg, task.ChangeMD5 && task.RandomFilename)
	if err != nil {
		return nil, err
	}
	if uploaded == nil {
		return nil, ErrUnsupportedMedia
	}

	rid2, err := randomID()
	if err != nil {
		return nil, err
	}
	caption2 := msg.Message
	entities2 := msg.Entities
	if out, truncated := sanitizeMediaCaptionText(caption2); truncated {
		caption2 = out
		entities2 = nil
	}
	req2 := &tg.MessagesSendMediaRequest{
		Peer:     peer,
		ReplyTo:  replyTo,
		Media:    uploaded,
		Message:  caption2,
		RandomID: rid2,
	}
	if len(entities2) > 0 {
		req2.Entities = entities2
	}
	upd, err := api.MessagesSendMedia(ctx, req2)
	return extractSentMsgIDs(upd), err
}
