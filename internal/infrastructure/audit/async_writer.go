package audit

import (
	"context"
	"log"

	"github.com/devravik/go-license-api/internal/domain"
)

// AsyncWriter enqueues audit entries without blocking request paths.
// If the queue is full, entries are dropped and logged.
type AsyncWriter struct {
	queue chan<- *domain.AuditEntry
}

func NewAsyncWriter(queue chan<- *domain.AuditEntry) *AsyncWriter {
	return &AsyncWriter{queue: queue}
}

func (w *AsyncWriter) Write(_ context.Context, entry *domain.AuditEntry) {
	if w == nil || w.queue == nil || entry == nil {
		return
	}
	select {
	case w.queue <- entry:
	default:
		log.Printf("audit drop: queue full tenant=%s event=%s resource=%s", entry.TenantID, entry.Event, entry.ResourceID)
	}
}

func (w *AsyncWriter) Flush() {}
