package workerpool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	ErrClosed    = errors.New("workerpool closed")
	ErrQueueFull = errors.New("workerpool queue full")
)

// Job is a unit of work executed by the Pool.
// The passed context is the per-job context, not the pool lifetime context.
type Job func(context.Context) error

type jobItem struct {
	ctx  context.Context
	fn   Job
	done chan error
}

// Future represents a submitted job result.
type Future struct {
	done chan error
}

func (f Future) Wait(ctx context.Context) error {
	if f.done == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-f.done:
		return err
	}
}

// Pool is a bounded goroutine worker pool with graceful shutdown.
// - maxWorkers controls concurrent execution.
// - queueSize controls backpressure.
//
// Submit blocks when the queue is full (unless ctx is done).
// TrySubmit returns ErrQueueFull when the queue is full.
type Pool struct {
	name string

	jobs chan jobItem

	closed    atomic.Bool
	closeOnce sync.Once

	workersWg sync.WaitGroup
}

func New(maxWorkers, queueSize int) *Pool {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if queueSize < 0 {
		queueSize = 0
	}

	p := &Pool{
		name: "workerpool",
		jobs: make(chan jobItem, queueSize),
	}

	p.workersWg.Add(maxWorkers)
	for i := 0; i < maxWorkers; i++ {
		go func(workerID int) {
			defer p.workersWg.Done()
			for item := range p.jobs {
				if item.done == nil {
					continue
				}
				if item.fn == nil {
					item.done <- errors.New("nil job")
					continue
				}
				ctx := item.ctx
				if ctx == nil {
					ctx = context.Background()
				}
				if err := ctx.Err(); err != nil {
					item.done <- err
					continue
				}
				item.done <- item.fn(ctx)
			}
		}(i + 1)
	}

	return p
}

func (p *Pool) WithName(name string) *Pool {
	if p == nil {
		return p
	}
	if name = strings.TrimSpace(name); name != "" {
		p.name = name
	}
	return p
}

func (p *Pool) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *Pool) IsClosed() bool {
	if p == nil {
		return true
	}
	return p.closed.Load()
}

func (p *Pool) Submit(ctx context.Context, fn Job) (Future, error) {
	if p == nil {
		return Future{}, ErrClosed
	}
	if p.closed.Load() {
		return Future{}, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	f := Future{done: make(chan error, 1)}
	item := jobItem{ctx: ctx, fn: fn, done: f.done}

	select {
	case <-ctx.Done():
		return Future{}, ctx.Err()
	case p.jobs <- item:
		return f, nil
	}
}

func (p *Pool) TrySubmit(ctx context.Context, fn Job) (Future, error) {
	if p == nil {
		return Future{}, ErrClosed
	}
	if p.closed.Load() {
		return Future{}, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	f := Future{done: make(chan error, 1)}
	item := jobItem{ctx: ctx, fn: fn, done: f.done}

	select {
	case <-ctx.Done():
		return Future{}, ctx.Err()
	case p.jobs <- item:
		return f, nil
	default:
		return Future{}, fmt.Errorf("%w: %s", ErrQueueFull, p.name)
	}
}

// Shutdown closes the pool and waits for all queued jobs to finish.
// It does not cancel in-flight jobs; use per-job contexts for cancelation.
func (p *Pool) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	p.closeOnce.Do(func() {
		p.closed.Store(true)
		close(p.jobs)
	})

	done := make(chan struct{})
	go func() {
		p.workersWg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
