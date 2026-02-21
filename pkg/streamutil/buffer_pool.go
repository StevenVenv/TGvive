package streamutil

import "sync"

// BufferPool reuses fixed-size byte buffers to reduce allocations/GC pressure.
// It is intended for streaming copy (io.CopyBuffer).
type BufferPool struct {
	size int
	pool sync.Pool // stores *[]byte
}

func NewBufferPool(size int) *BufferPool {
	if size <= 0 {
		size = 32 * 1024
	}
	bp := &BufferPool{size: size}
	bp.pool.New = func() any {
		b := make([]byte, bp.size)
		return &b
	}
	return bp
}

func (p *BufferPool) Size() int {
	if p == nil {
		return 0
	}
	return p.size
}

func (p *BufferPool) Get() *[]byte {
	if p == nil || p.size <= 0 {
		b := make([]byte, 32*1024)
		return &b
	}

	b, ok := p.pool.Get().(*[]byte)
	if !ok || b == nil {
		nb := make([]byte, p.size)
		return &nb
	}

	if cap(*b) < p.size {
		*b = make([]byte, p.size)
	} else {
		*b = (*b)[:p.size]
	}
	return b
}

func (p *BufferPool) Put(b *[]byte) {
	if p == nil || p.size <= 0 || b == nil || *b == nil {
		return
	}
	if cap(*b) < p.size {
		return
	}

	*b = (*b)[:p.size]
	p.pool.Put(b)
}

var DefaultBufferPool = NewBufferPool(32 * 1024)
