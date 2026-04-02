package db

import (
	"context"
	"encoding/json"
	"log"
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

func (w *AuditWriter) Flush() {}

func defaultText(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
