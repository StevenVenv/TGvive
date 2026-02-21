package streamutil

import "sync"

// BufferPool reuses fixed-size byte buffers to reduce allocations/GC pressure.
// It is intended for streaming copy (io.CopyBuffer).
type BufferPool struct {
	size int
	pool sync.Pool // stores []byte
}

func NewBufferPool(size int) *BufferPool {
	if size <= 0 {
		size = 32 * 1024
	}
	bp := &BufferPool{size: size}
	bp.pool.New = func() any {
		return make([]byte, bp.size)
	}
	return bp
}

func (p *BufferPool) Size() int {
	if p == nil {
		return 0
	}
	return p.size
}

func (p *BufferPool) Get() []byte {
	if p == nil || p.size <= 0 {
		return make([]byte, 32*1024)
	}
	b, ok := p.pool.Get().([]byte)
	if !ok || cap(b) < p.size {
		return make([]byte, p.size)
	}
	return b[:p.size]
}

func (p *BufferPool) Put(b []byte) {
	if p == nil || p.size <= 0 || b == nil {
		return
	}
	if cap(b) < p.size {
		return
	}
	p.pool.Put(b[:p.size])
}

var DefaultBufferPool = NewBufferPool(32 * 1024)
