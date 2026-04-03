package db

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditWriter struct {
	pool *pgxpool.Pool
}

func NewAuditWriter(pool *pgxpool.Pool) *AuditWriter {
	return &AuditWriter{pool: pool}
}

func (w *AuditWriter) Write(ctx context.Context, entry *domain.AuditEntry) {
	if w == nil || w.pool == nil || entry == nil {
		return
	}
	id := entry.ID
	if id == "" {
		id = uuid.NewString()
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	meta, err := json.Marshal(entry.Meta)
	if err != nil {
		meta = []byte("{}")
	}
	_, execErr := w.pool.Exec(ctx, `
		INSERT INTO audit_log (id, tenant_id, actor_id, actor_ip, event, resource_id, outcome, meta, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, id, entry.TenantID, defaultText(entry.ActorID, "system"), defaultText(entry.ActorIP, "0.0.0.0"), entry.Event, entry.ResourceID, entry.Outcome, meta, createdAt)
	if execErr != nil {
		log.Printf("audit insert failed: %v", execErr)
	}
}

// WriteBatch persists audit entries in a single multi-values INSERT.
func (w *AuditWriter) WriteBatch(ctx context.Context, entries []*domain.AuditEntry) {
	if w == nil || w.pool == nil || len(entries) == 0 {
		return
	}
	args := make([]any, 0, len(entries)*9)
	var sb strings.Builder
	sb.WriteString("INSERT INTO audit_log (id, tenant_id, actor_id, actor_ip, event, resource_id, outcome, meta, created_at) VALUES ")
	placeholder := 1
	valid := 0
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		id := entry.ID
		if id == "" {
			id = uuid.NewString()
		}
		createdAt := entry.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		meta, err := json.Marshal(entry.Meta)
		if err != nil {
			meta = []byte("{}")
		}
		if valid > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("($")
		sb.WriteString(intToString(placeholder))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 1))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 2))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 3))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 4))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 5))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 6))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 7))
		sb.WriteString(",$")
		sb.WriteString(intToString(placeholder + 8))
		sb.WriteString(")")
		placeholder += 9
		valid++
		args = append(args,
			id,
			entry.TenantID,
			defaultText(entry.ActorID, "system"),
			defaultText(entry.ActorIP, "0.0.0.0"),
			entry.Event,
			entry.ResourceID,
			entry.Outcome,
			meta,
			createdAt,
		)
	}
	if valid == 0 {
		return
	}
	if _, err := w.pool.Exec(ctx, sb.String(), args...); err != nil {
		log.Printf("audit batch insert failed count=%d err=%v", valid, err)
	}
}

func (w *AuditWriter) Flush() {}

// FlushWithContext allows callers to enforce a timeout during shutdown.
// Current implementation is a no-op as writes are synchronous, but the API
// ensures shutdown cannot block on audit flushing if implementations change.
func (w *AuditWriter) FlushWithContext(ctx context.Context) {}

func defaultText(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func intToString(i int) string {
	return strconv.Itoa(i)
}
