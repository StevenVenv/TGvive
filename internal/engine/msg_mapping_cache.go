package engine

import (
	"container/list"
	"sync"
)

type msgMappingCache struct {
	mu  sync.Mutex
	cap int

	ll *list.List
	m  map[int]*list.Element
}

type msgMappingEntry struct {
	src int
	dst int
}

func newMsgMappingCache(capacity int) *msgMappingCache {
	if capacity <= 0 {
		capacity = 10_000
	}
	return &msgMappingCache{
		cap: capacity,
		ll:  list.New(),
		m:   make(map[int]*list.Element, minInt(capacity, 1024)),
	}
}

func (c *msgMappingCache) Get(src int) (dst int, ok bool) {
	if c == nil || src <= 0 {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.m[src]
	if el == nil {
		return 0, false
	}
	ent, _ := el.Value.(*msgMappingEntry)
	if ent == nil || ent.dst <= 0 {
		return 0, false
	}
	c.ll.MoveToFront(el)
	return ent.dst, true
}

func (c *msgMappingCache) Put(src int, dst int) {
	if c == nil || src <= 0 || dst <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if el := c.m[src]; el != nil {
		if ent, _ := el.Value.(*msgMappingEntry); ent != nil {
			ent.dst = dst
		} else {
			el.Value = &msgMappingEntry{src: src, dst: dst}
		}
		c.ll.MoveToFront(el)
		return
	}

	el := c.ll.PushFront(&msgMappingEntry{src: src, dst: dst})
	c.m[src] = el

	for c.cap > 0 && c.ll.Len() > c.cap {
		back := c.ll.Back()
		if back == nil {
			break
		}
		ent, _ := back.Value.(*msgMappingEntry)
		if ent != nil {
			delete(c.m, ent.src)
		}
		c.ll.Remove(back)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
