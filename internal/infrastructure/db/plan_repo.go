package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type planRepo struct {
	db *pgxpool.Pool
}

func NewPlanRepo(db *pgxpool.Pool) domain.PlanRepository {
	return &planRepo{db: db}
}

func (r *planRepo) FindByID(ctx context.Context, tenantID, planID string) (*domain.Plan, error) {
	const q = `
		SELECT id, tenant_id, product_id, name, features, limits, is_active, created_at, updated_at
		FROM plans WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL LIMIT 1
	`
	row := r.db.QueryRow(ctx, q, tenantID, planID)
	p := &domain.Plan{}
	if err := row.Scan(&p.ID, &p.TenantID, &p.ProductID, &p.Name, &p.Features, &p.Limits, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrProductNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *planRepo) ListByTenant(ctx context.Context, tenantID string) ([]*domain.Plan, error) {
	const q = `
		SELECT id, tenant_id, product_id, name, features, limits, is_active, created_at, updated_at
		FROM plans WHERE tenant_id = $1 AND deleted_at IS NULL ORDER BY name ASC
	`
	rows, err := r.db.Query(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*domain.Plan, 0)
	for rows.Next() {
		p := &domain.Plan{}
		if err := rows.Scan(&p.ID, &p.TenantID, &p.ProductID, &p.Name, &p.Features, &p.Limits, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *planRepo) Upsert(ctx context.Context, p *domain.Plan) error {
	const q = `
		INSERT INTO plans (id, tenant_id, product_id, name, features, limits, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET
			product_id = EXCLUDED.product_id,
			name = EXCLUDED.name,
			features = EXCLUDED.features,
			limits = EXCLUDED.limits,
			is_active = EXCLUDED.is_active
	`
	_, err := r.db.Exec(ctx, q, p.ID, p.TenantID, p.ProductID, p.Name, p.Features, p.Limits, p.IsActive)
	return err
}

func (r *planRepo) SetActive(ctx context.Context, tenantID, planID string, isActive bool) error {
	const q = `UPDATE plans SET is_active = $3 WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, tenantID, planID, isActive)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("plan not found")
	}
	return nil
}

func (r *planRepo) Delete(ctx context.Context, tenantID, planID string) error {
	const q = `UPDATE plans SET deleted_at = NOW() WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL`
	tag, err := r.db.Exec(ctx, q, tenantID, planID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("plan not found")
	}
	return nil
}

func (r *planRepo) Restore(ctx context.Context, tenantID, planID string) error {
	const q = `UPDATE plans SET deleted_at = NULL WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NOT NULL`
	tag, err := r.db.Exec(ctx, q, tenantID, planID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("plan not found")
	}
	return nil
}
