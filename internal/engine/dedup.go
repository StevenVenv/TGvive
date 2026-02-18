package engine

import (
	"sync"
	"time"
)

type dedupKey struct {
	TaskID uint
	MsgID  int
}

type dedupEntry struct {
	key dedupKey
	exp int64
}

// Deduper is a small in-memory recent-message de-dup cache.
// It is safe for concurrent use.
type Deduper struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxKeys int

	entries map[dedupKey]int64
	order   []dedupEntry
	head    int
}

func NewDeduper(ttl time.Duration, maxKeys int) *Deduper {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if maxKeys <= 0 {
		maxKeys = 50_000
	}
	return &Deduper{
		ttl:     ttl,
		maxKeys: maxKeys,
		entries: make(map[dedupKey]int64, maxKeys/2),
	}
}

// Seen reports whether (taskID,msgID) has been seen recently.
// If not seen, it marks it as seen and returns false.
func (d *Deduper) Seen(taskID uint, msgID int) bool {
	if d == nil {
		return false
	}
	if taskID == 0 || msgID <= 0 {
		return false
	}

	now := time.Now().UnixNano()
	exp := now + int64(d.ttl)
	key := dedupKey{TaskID: taskID, MsgID: msgID}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.pruneLocked(now)

	if prev, ok := d.entries[key]; ok && prev > now {
		return true
	}

	d.entries[key] = exp
	d.order = append(d.order, dedupEntry{key: key, exp: exp})
	d.evictLocked(now)
	return false
}

func (d *Deduper) DropTask(taskID uint) {
	if d == nil || taskID == 0 {
		return
	}

	d.mu.Lock()
	for k := range d.entries {
		if k.TaskID == taskID {
			delete(d.entries, k)
		}
	}
	d.mu.Unlock()
}

// Forget removes a previously-seen key so it may be processed again.
// It is safe to call even if the key does not exist.
func (d *Deduper) Forget(taskID uint, msgID int) {
	if d == nil {
		return
	}
	if taskID == 0 || msgID <= 0 {
		return
	}

	key := dedupKey{TaskID: taskID, MsgID: msgID}
	d.mu.Lock()
	delete(d.entries, key)
	d.mu.Unlock()
}

func (d *Deduper) pruneLocked(now int64) {
	for d.head < len(d.order) {
		ent := d.order[d.head]
		if ent.exp > now {
			break
		}
		if cur, ok := d.entries[ent.key]; ok && cur == ent.exp {
			delete(d.entries, ent.key)
		}
		d.head++
	}

	// Compact occasionally.
	if d.head > 2048 && d.head > len(d.order)/2 {
		d.order = append([]dedupEntry(nil), d.order[d.head:]...)
		d.head = 0
	}
}

func (d *Deduper) evictLocked(now int64) {
	if len(d.entries) <= d.maxKeys {
		return
	}

	for len(d.entries) > d.maxKeys && d.head < len(d.order) {
		ent := d.order[d.head]
		if cur, ok := d.entries[ent.key]; ok && cur == ent.exp {
			delete(d.entries, ent.key)
		}
		d.head++
	}

	d.pruneLocked(now)
}
