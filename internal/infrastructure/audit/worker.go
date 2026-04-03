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

const (
	auditBatchSize    = 100
	auditBatchTimeout = 50 * time.Millisecond
)

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
	timer := time.NewTimer(auditBatchTimeout)
	defer timer.Stop()
	batch := make([]*domain.AuditEntry, 0, auditBatchSize)
	for {
		select {
		case <-ctx.Done():
			w.writeBatchWithRetry(ctx, batch)
			return
		case entry, ok := <-w.queue:
			if !ok {
				w.writeBatchWithRetry(ctx, batch)
				return
			}
			if entry == nil || w.writer == nil {
				continue
			}
			batch = append(batch, entry)
			if len(batch) >= auditBatchSize {
				w.writeBatchWithRetry(ctx, batch)
				batch = batch[:0]
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(auditBatchTimeout)
			}
		case <-timer.C:
			if len(batch) > 0 {
				w.writeBatchWithRetry(ctx, batch)
				batch = batch[:0]
			}
			timer.Reset(auditBatchTimeout)
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

func (w *Worker) writeBatchWithRetry(ctx context.Context, entries []*domain.AuditEntry) {
	if len(entries) == 0 {
		return
	}
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		if safeWriteBatch(w.writer, ctx, entries) {
			return
		}
		if attempt == w.maxRetries || w.retryDelay <= 0 {
			return
		}
		timer := time.NewTimer(w.retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
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

func safeWriteBatch(writer domain.AuditWriter, ctx context.Context, entries []*domain.AuditEntry) (ok bool) {
	ok = true
	defer func() {
		if r := recover(); r != nil {
			ok = false
			log.Printf("audit batch write panic recovered: %v", r)
		}
	}()
	if bw, yes := writer.(interface {
		WriteBatch(context.Context, []*domain.AuditEntry)
	}); yes {
		bw.WriteBatch(ctx, entries)
		return ok
	}
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		writer.Write(ctx, entry)
	}
	return ok
}
