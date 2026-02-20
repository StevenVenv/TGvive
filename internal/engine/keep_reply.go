package engine

import (
	"sort"
	"sync"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	"github.com/gotd/td/tg"
	"go.uber.org/zap"
	"gorm.io/gorm/clause"
)

var msgMappingCaches sync.Map // map[uint]*msgMappingCache

func msgMappingCacheForTask(taskID uint) *msgMappingCache {
	if taskID == 0 {
		return nil
	}
	if v, ok := msgMappingCaches.Load(taskID); ok {
		if c, ok := v.(*msgMappingCache); ok && c != nil {
			return c
		}
	}
	c := newMsgMappingCache(50_000)
	if v, _ := msgMappingCaches.LoadOrStore(taskID, c); v != nil {
		if cc, ok := v.(*msgMappingCache); ok && cc != nil {
			return cc
		}
	}
	return c
}

func clearMsgMappingCache(taskID uint) {
	if taskID == 0 {
		return
	}
	msgMappingCaches.Delete(taskID)
}

func extractReplyToSourceMsgID(msg *tg.Message) int {
	if msg == nil || msg.ReplyTo == nil {
		return 0
	}
	h, ok := msg.ReplyTo.(*tg.MessageReplyHeader)
	if !ok || h == nil {
		return 0
	}
	if mid, ok := h.GetReplyToMsgID(); ok && mid > 0 {
		return mid
	}
	if top, ok := h.GetReplyToTopID(); ok && top > 0 {
		return top
	}
	return 0
}

func buildKeepReplyInput(task model.Task, msg *tg.Message) tg.InputReplyToClass {
	if !task.KeepReply || task.ID == 0 || msg == nil {
		return nil
	}
	srcReplyID := extractReplyToSourceMsgID(msg)
	if srcReplyID <= 0 {
		return nil
	}
	dstReplyID := lookupTargetMsgID(task.ID, srcReplyID)
	if dstReplyID <= 0 {
		return nil
	}
	return &tg.InputReplyToMessage{ReplyToMsgID: dstReplyID}
}

func lookupTargetMsgID(taskID uint, srcMsgID int) int {
	if taskID == 0 || srcMsgID <= 0 {
		return 0
	}
	if c := msgMappingCacheForTask(taskID); c != nil {
		if dst, ok := c.Get(srcMsgID); ok && dst > 0 {
			return dst
		}
	}

	db := localdb.Default.Get(taskID)
	if db == nil {
		return 0
	}

	var row localdb.LocalMsgMapping
	if err := db.Where("source_msg_id = ?", srcMsgID).First(&row).Error; err != nil {
		return 0
	}
	if row.TargetMsgID <= 0 {
		return 0
	}
	if c := msgMappingCacheForTask(taskID); c != nil {
		c.Put(srcMsgID, row.TargetMsgID)
	}
	return row.TargetMsgID
}

func storeMsgMapping(task model.Task, srcMsgID, dstMsgID int) {
	if !task.KeepReply || task.ID == 0 || srcMsgID <= 0 || dstMsgID <= 0 {
		return
	}
	if c := msgMappingCacheForTask(task.ID); c != nil {
		c.Put(srcMsgID, dstMsgID)
	}

	db := localdb.Default.Get(task.ID)
	if db == nil {
		return
	}

	row := localdb.LocalMsgMapping{
		SourceMsgID: srcMsgID,
		TargetMsgID: dstMsgID,
	}
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source_msg_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"target_msg_id": dstMsgID,
			"updated_at":    time.Now(),
		}),
	}).Create(&row).Error; err != nil && global.Logger != nil {
		global.Logger.Debug(
			"store msg mapping failed",
			zap.Uint("task_id", task.ID),
			zap.Int("src_msg_id", srcMsgID),
			zap.Int("dst_msg_id", dstMsgID),
			zap.Error(err),
		)
	}
}

func storeMsgMappingsInOrder(task model.Task, srcMsgs []*tg.Message, sentIDs []int) {
	if !task.KeepReply || task.ID == 0 {
		return
	}
	if len(srcMsgs) == 0 || len(sentIDs) == 0 {
		return
	}

	srcIDs := make([]int, 0, len(srcMsgs))
	for _, m := range srcMsgs {
		if m == nil || m.ID <= 0 {
			continue
		}
		srcIDs = append(srcIDs, m.ID)
	}
	if len(srcIDs) == 0 {
		return
	}

	sort.Ints(srcIDs)
	sort.Ints(sentIDs)
	if len(srcIDs) != len(sentIDs) {
		if global.Logger != nil {
			global.Logger.Debug(
				"sent id count mismatch, skip keep-reply mapping for batch",
				zap.Uint("task_id", task.ID),
				zap.Int("src_count", len(srcIDs)),
				zap.Int("sent_count", len(sentIDs)),
			)
		}
		return
	}

	for i := 0; i < len(srcIDs); i++ {
		storeMsgMapping(task, srcIDs[i], sentIDs[i])
	}
}
