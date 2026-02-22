package engine

import (
	"strings"

	"my-go-server/internal/global"

	"github.com/gotd/td/tg"
)

type publisherKind uint8

const (
	publisherKindMTProto publisherKind = iota + 1
	publisherKindBot
)

type taskPublisherRuntime struct {
	Kind publisherKind

	// MTProto publisher.
	API  *tg.Client
	Peer tg.InputPeerClass

	// Bot publisher.
	Bot    global.StoredBot
	ChatID string
}

func (p *taskPublisherRuntime) isBot() bool {
	return p != nil && p.Kind == publisherKindBot
}

func (p *taskPublisherRuntime) isMTProto() bool {
	return p != nil && p.Kind == publisherKindMTProto
}

func (p *taskPublisherRuntime) describe() string {
	if p == nil {
		return ""
	}
	switch p.Kind {
	case publisherKindMTProto:
		if p.Peer != nil {
			return "account"
		}
		return "account(?)"
	case publisherKindBot:
		name := strings.TrimSpace(p.Bot.Name)
		if name != "" {
			return "bot:" + name
		}
		id := strings.TrimSpace(p.Bot.ID)
		if id != "" {
			return "bot:" + id
		}
		return "bot"
	default:
		return "unknown"
	}
}

func (m *TaskManager) setPublisher(taskID uint, pub *taskPublisherRuntime) {
	if m == nil || taskID == 0 {
		return
	}
	m.pubMu.Lock()
	if m.publishers == nil {
		m.publishers = make(map[uint]*taskPublisherRuntime)
	}
	if pub == nil {
		delete(m.publishers, taskID)
	} else {
		m.publishers[taskID] = pub
	}
	m.pubMu.Unlock()
}

func (m *TaskManager) getPublisher(taskID uint) *taskPublisherRuntime {
	if m == nil || taskID == 0 {
		return nil
	}
	m.pubMu.RLock()
	pub := m.publishers[taskID]
	m.pubMu.RUnlock()
	return pub
}

func (m *TaskManager) clearPublisher(taskID uint) {
	m.setPublisher(taskID, nil)
}
