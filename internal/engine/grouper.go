package engine

import (
	"sync"
	"time"

	"github.com/gotd/td/tg"
)

type albumKey struct {
	TaskID    uint
	GroupedID int64
}

type albumGroup struct {
	timer   *time.Timer
	timerID uint64
	msgs    []*tg.Message
}

// AlbumGrouper aggregates telegram album messages (same GroupedID) with a debounce window.
// It is safe for concurrent use.
type AlbumGrouper struct {
	mu     sync.Mutex
	window time.Duration
	seq    uint64
	groups map[albumKey]*albumGroup
}

func NewAlbumGrouper(window time.Duration) *AlbumGrouper {
	if window <= 0 {
		window = 120 * time.Millisecond
	}
	return &AlbumGrouper{
		window: window,
		groups: make(map[albumKey]*albumGroup),
	}
}

func (g *AlbumGrouper) Add(taskID uint, groupedID int64, msg *tg.Message, flush func([]*tg.Message)) {
	if g == nil || msg == nil || flush == nil {
		return
	}
	if taskID == 0 || groupedID == 0 {
		flush([]*tg.Message{msg})
		return
	}

	key := albumKey{TaskID: taskID, GroupedID: groupedID}

	g.mu.Lock()
	grp := g.groups[key]
	if grp == nil {
		grp = &albumGroup{}
		g.groups[key] = grp
	}

	if msg.ID != 0 {
		for _, existing := range grp.msgs {
			if existing != nil && existing.ID == msg.ID {
				// duplicate update
				g.mu.Unlock()
				return
			}
		}
	}
	grp.msgs = append(grp.msgs, msg)

	if grp.timer != nil {
		_ = grp.timer.Stop()
	}

	g.seq++
	timerID := g.seq
	grp.timerID = timerID
	grp.timer = time.AfterFunc(g.window, func() {
		g.fire(key, timerID, flush)
	})
	g.mu.Unlock()
}

func (g *AlbumGrouper) fire(key albumKey, timerID uint64, flush func([]*tg.Message)) {
	var msgs []*tg.Message

	g.mu.Lock()
	grp := g.groups[key]
	if grp == nil || grp.timerID != timerID {
		g.mu.Unlock()
		return
	}
	msgs = grp.msgs
	delete(g.groups, key)
	g.mu.Unlock()

	if len(msgs) == 0 {
		return
	}
	flush(msgs)
}

func (g *AlbumGrouper) FlushTask(taskID uint, flush func([]*tg.Message)) {
	if g == nil || taskID == 0 || flush == nil {
		return
	}

	var batches [][]*tg.Message

	g.mu.Lock()
	for k, grp := range g.groups {
		if k.TaskID != taskID {
			continue
		}
		if grp.timer != nil {
			_ = grp.timer.Stop()
		}
		if len(grp.msgs) > 0 {
			batches = append(batches, grp.msgs)
		}
		delete(g.groups, k)
	}
	g.mu.Unlock()

	for _, msgs := range batches {
		flush(msgs)
	}
}

func (g *AlbumGrouper) DropTask(taskID uint) {
	if g == nil || taskID == 0 {
		return
	}

	g.mu.Lock()
	for k, grp := range g.groups {
		if k.TaskID != taskID {
			continue
		}
		if grp.timer != nil {
			_ = grp.timer.Stop()
		}
		delete(g.groups, k)
	}
	g.mu.Unlock()
}
