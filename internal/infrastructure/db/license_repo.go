package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type licenseRepo struct {
	db *pgxpool.Pool
}

func NewLicenseRepo(db *pgxpool.Pool) domain.LicenseRepository {
	return &licenseRepo{db: db}
}

func (r *licenseRepo) FindByKey(ctx context.Context, tenantID, key string) (*domain.License, error) {
	const q = `
		SELECT
			id, tenant_id, key,
			product_id, product,
			status, plan, is_trial,
			trial_ends_at, expires_at, grace_period_days,
			seat_count, max_activations,
			usage_limit, usage_used,
			features, meta, created_at
		FROM licenses
		WHERE tenant_id = $1 AND key = $2
		LIMIT 1
	`

	l := &domain.License{}
	row := r.db.QueryRow(ctx, q, tenantID, key)
	if err := row.Scan(
		&l.ID,
		&l.TenantID,
		&l.Key,
		&l.ProductID,
		&l.Product,
		&l.Status,
		&l.Plan,
		&l.IsTrial,
		&l.TrialEndsAt,
		&l.ExpiresAt,
		&l.GracePeriodDays,
		&l.SeatCount,
		&l.MaxActivations,
		&l.UsageLimit,
		&l.UsageUsed,
		&l.Features,
		&l.Meta,
		&l.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrLicenseNotFound
		}
		return nil, fmt.Errorf("license scan: %w", err)
	}

	return l, nil
}

func (r *licenseRepo) Create(ctx context.Context, l *domain.License) error {
	const q = `
		INSERT INTO licenses (
			tenant_id, key, product_id, product,
			status, plan, is_trial,
			trial_ends_at, expires_at, grace_period_days,
			seat_count, max_activations,
			usage_limit, usage_used,
			features, meta
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10,
			$11, $12,
			$13, $14,
			$15, $16
		)
		RETURNING id
	`

	return r.db.QueryRow(ctx, q,
		l.TenantID, l.Key, l.ProductID, l.Product,
		l.Status, l.Plan, l.IsTrial,
		l.TrialEndsAt, l.ExpiresAt, l.GracePeriodDays,
		l.SeatCount, l.MaxActivations,
		l.UsageLimit, l.UsageUsed,
		l.Features, l.Meta,
	).Scan(&l.ID)
}

func (r *licenseRepo) Revoke(ctx context.Context, tenantID, key string) error {
	const q = `UPDATE licenses SET status = 'revoked' WHERE tenant_id = $1 AND key = $2`
	_, err := r.db.Exec(ctx, q, tenantID, key)
	return err
}

func (r *licenseRepo) Update(ctx context.Context, l *domain.License) error {
	const q = `
		UPDATE licenses SET
			product_id = $3,
			product = $4,
			status = $5,
			plan = $6,
			is_trial = $7,
			trial_ends_at = $8,
			expires_at = $9,
			grace_period_days = $10,
			seat_count = $11,
			max_activations = $12,
			usage_limit = $13,
			usage_used = $14,
			features = $15,
			meta = $16
		WHERE tenant_id = $1 AND key = $2
	`
	_, err := r.db.Exec(ctx, q,
		l.TenantID, l.Key,
		l.ProductID, l.Product,
		l.Status, l.Plan, l.IsTrial,
		l.TrialEndsAt, l.ExpiresAt, l.GracePeriodDays,
		l.SeatCount, l.MaxActivations,
		l.UsageLimit, l.UsageUsed,
		l.Features, l.Meta,
	)
	return err
}

func (r *licenseRepo) ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]*domain.License, error) {
	const q = `
		SELECT
			id, tenant_id, key,
			product_id, product,
			status, plan, is_trial,
			trial_ends_at, expires_at, grace_period_days,
			seat_count, max_activations,
			usage_limit, usage_used,
			features, meta, created_at
		FROM licenses
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, q, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("licenses by tenant query: %w", err)
	}
	defer rows.Close()
	var out []*domain.License
	for rows.Next() {
		var l domain.License
		if err := rows.Scan(
			&l.ID,
			&l.TenantID,
			&l.Key,
			&l.ProductID,
			&l.Product,
			&l.Status,
			&l.Plan,
			&l.IsTrial,
			&l.TrialEndsAt,
			&l.ExpiresAt,
			&l.GracePeriodDays,
			&l.SeatCount,
			&l.MaxActivations,
			&l.UsageLimit,
			&l.UsageUsed,
			&l.Features,
			&l.Meta,
			&l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("license scan: %w", err)
		}
		cp := l
		out = append(out, &cp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("licenses rows: %w", err)
	}
	return out, nil
}

func (r *licenseRepo) GetRecent(ctx context.Context, limit int) ([]domain.License, error) {
	const q = `
		SELECT
			id, tenant_id, key,
			product_id, product,
			status, plan, is_trial,
			trial_ends_at, expires_at, grace_period_days,
			seat_count, max_activations,
			usage_limit, usage_used,
			features, meta, created_at
		FROM licenses
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("recent licenses query: %w", err)
	}
	defer rows.Close()

	out := make([]domain.License, 0, limit)
	for rows.Next() {
		var l domain.License
		if err := rows.Scan(
			&l.ID,
			&l.TenantID,
			&l.Key,
			&l.ProductID,
			&l.Product,
			&l.Status,
			&l.Plan,
			&l.IsTrial,
			&l.TrialEndsAt,
			&l.ExpiresAt,
			&l.GracePeriodDays,
			&l.SeatCount,
			&l.MaxActivations,
			&l.UsageLimit,
			&l.UsageUsed,
			&l.Features,
			&l.Meta,
			&l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("recent license scan: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent licenses rows: %w", err)
	}
	return out, nil
}
