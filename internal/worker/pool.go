package worker

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/devravik/go-license-api/internal/app"
)

type Pool struct {
	queue      chan Job
	wg         sync.WaitGroup
	validation app.ValidationService
	workers    int
	restarts   int64
}

func NewPool(workers, queueSize int, validation app.ValidationService) *Pool {
	return &Pool{
		queue:      make(chan Job, queueSize),
		validation: validation,
		workers:    workers,
	}
}

func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			for {
				func() {
					defer func() {
						if recover() != nil {
							atomic.AddInt64(&p.restarts, 1)
						}
					}()
					w := newWorker(id, p.queue, p.validation)
					w.run(ctx)
				}()
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}(i)
	}
}

func (p *Pool) Enqueue(job Job) bool {
	select {
	case p.queue <- job:
		return true
	default:
		return false
	}
}

func (p *Pool) QueueDepth() int {
	return len(p.queue)
}

func (p *Pool) QueueCapacity() int {
	return cap(p.queue)
}

func (p *Pool) Restarts() int64 {
	return atomic.LoadInt64(&p.restarts)
}

func (p *Pool) Drain(ctx context.Context) {
	close(p.queue)
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}
