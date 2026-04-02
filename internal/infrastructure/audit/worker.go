package audit

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

type Worker struct {
	writer      domain.AuditWriter
	queue       <-chan *domain.AuditEntry
	workers     int
	maxRetries  int
	retryDelay  time.Duration
	waitGroup   sync.WaitGroup
}

func NewWorker(writer domain.AuditWriter, queue <-chan *domain.AuditEntry, workers, maxRetries int, retryDelay time.Duration) *Worker {
	if workers < 1 {
		workers = 1
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &Worker{
		writer:     writer,
		queue:      queue,
		workers:    workers,
		maxRetries: maxRetries,
		retryDelay: retryDelay,
	}
}

func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.workers; i++ {
		w.waitGroup.Add(1)
		go w.run(ctx)
	}
}

func (w *Worker) Wait() {
	w.waitGroup.Wait()
}

func (w *Worker) run(ctx context.Context) {
	defer w.waitGroup.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-w.queue:
			if !ok {
				return
			}
			if entry == nil || w.writer == nil {
				continue
			}
			w.writeWithRetry(ctx, entry)
		}
	}
}

func (w *Worker) writeWithRetry(ctx context.Context, entry *domain.AuditEntry) {
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		if safeWrite(w.writer, ctx, entry) {
			return
		}
		if attempt == w.maxRetries || w.retryDelay <= 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.retryDelay):
		}
	}
}

func safeWrite(writer domain.AuditWriter, ctx context.Context, entry *domain.AuditEntry) (ok bool) {
	ok = true
	defer func() {
		if r := recover(); r != nil {
			ok = false
			log.Printf("audit write panic recovered: %v", r)
		}
	}()
	writer.Write(ctx, entry)
	return ok
}
