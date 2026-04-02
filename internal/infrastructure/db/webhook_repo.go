package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type WebhookRepo struct {
	pool *pgxpool.Pool
}

func NewWebhookRepo(pool *pgxpool.Pool) *WebhookRepo {
	return &WebhookRepo{pool: pool}
}

func (r *WebhookRepo) Create(ctx context.Context, id, tenantID, url string, events []string, secretEnc []byte) error {
	const q = `INSERT INTO webhooks (id, tenant_id, url, events, secret_enc, is_active) VALUES ($1, $2, $3, $4, $5, TRUE)`
	_, err := r.pool.Exec(ctx, q, id, tenantID, url, events, secretEnc)
	return err
}

