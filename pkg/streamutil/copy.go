package streamutil

import (
	"context"
	"io"
)

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr ctxReader) Read(p []byte) (int, error) {
	if cr.ctx != nil {
		if err := cr.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return cr.r.Read(p)
}

type ctxWriter struct {
	ctx context.Context
	w   io.Writer
}

func (cw ctxWriter) Write(p []byte) (int, error) {
	if cw.ctx != nil {
		if err := cw.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return cw.w.Write(p)
}

// Copy streams from src to dst using DefaultBufferPool (32KB) and context cancellation.
func Copy(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	return CopyWithPool(ctx, dst, src, DefaultBufferPool)
}

// CopyWithPool streams from src to dst using io.CopyBuffer with a pooled fixed-size buffer.
func CopyWithPool(ctx context.Context, dst io.Writer, src io.Reader, pool *BufferPool) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if dst == nil || src == nil {
		return 0, io.ErrClosedPipe
	}

	buf := pool.Get()
	defer pool.Put(buf)

	return io.CopyBuffer(ctxWriter{ctx: ctx, w: dst}, ctxReader{ctx: ctx, r: src}, buf)
}
