package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryService provides read access to the audit log for admin endpoints.
type QueryService struct {
	db *pgxpool.Pool
}

func NewQueryService(db *pgxpool.Pool) *QueryService {
	return &QueryService{db: db}
}

type QueryParams struct {
	TenantID string
	Event    string
	From     time.Time
	To       time.Time
	Limit    int
}

func (s *QueryService) Query(ctx context.Context, params QueryParams) ([]*domain.AuditEntry, error) {
	if params.Limit == 0 || params.Limit > 500 {
		params.Limit = 100
	}

	const q = `
        SELECT id, tenant_id, actor_id, actor_ip, event, resource_id, outcome, meta, created_at
        FROM audit_log
        WHERE ($1 = '' OR tenant_id = $1)
          AND ($2 = '' OR event = $2)
          AND ($3::TIMESTAMP IS NULL OR created_at >= $3)
          AND ($4::TIMESTAMP IS NULL OR created_at <= $4)
        ORDER BY created_at DESC
        LIMIT $5
    `
	rows, err := s.db.Query(ctx, q,
		params.TenantID, params.Event,
		nullTime(params.From), nullTime(params.To),
		params.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("audit query: %w", err)
	}
	defer rows.Close()

	var entries []*domain.AuditEntry
	for rows.Next() {
		e := &domain.AuditEntry{}
		err := rows.Scan(&e.ID, &e.TenantID, &e.ActorID, &e.ActorIP,
			&e.Event, &e.ResourceID, &e.Outcome, &e.Meta, &e.CreatedAt)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

