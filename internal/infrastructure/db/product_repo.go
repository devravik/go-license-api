package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type productRepo struct {
	db *pgxpool.Pool
}

func NewProductRepo(db *pgxpool.Pool) domain.ProductRepository {
	return &productRepo{db: db}
}

func (r *productRepo) FindByID(ctx context.Context, tenantID, productID string) (*domain.Product, error) {
	const q = `
		SELECT
			id, tenant_id, code, name, version, is_active,
			features, meta, created_at, updated_at
		FROM products
		WHERE tenant_id = $1 AND id = $2
		LIMIT 1
	`
	return r.scanOne(ctx, q, tenantID, productID)
}

func (r *productRepo) FindByCode(ctx context.Context, tenantID, code string) (*domain.Product, error) {
	const q = `
		SELECT
			id, tenant_id, code, name, version, is_active,
			features, meta, created_at, updated_at
		FROM products
		WHERE tenant_id = $1 AND code = $2
		LIMIT 1
	`
	return r.scanOne(ctx, q, tenantID, code)
}

func (r *productRepo) ListByTenant(ctx context.Context, tenantID string) ([]*domain.Product, error) {
	const q = `
		SELECT
			id, tenant_id, code, name, version, is_active,
			features, meta, created_at, updated_at
		FROM products
		WHERE tenant_id = $1
		ORDER BY code ASC
	`

	rows, err := r.db.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("products query: %w", err)
	}
	defer rows.Close()

	return r.scanMany(rows)
}

func (r *productRepo) ListUpdatedAfter(ctx context.Context, tenantID string, after time.Time) ([]*domain.Product, error) {
	const q = `
		SELECT
			id, tenant_id, code, name, version, is_active,
			features, meta, created_at, updated_at
		FROM products
		WHERE tenant_id = $1 AND updated_at > $2
		ORDER BY updated_at ASC
	`

	rows, err := r.db.Query(ctx, q, tenantID, after)
	if err != nil {
		return nil, fmt.Errorf("products query updated_after: %w", err)
	}
	defer rows.Close()

	return r.scanMany(rows)
}

func (r *productRepo) Upsert(ctx context.Context, p *domain.Product) error {
	const q = `
		INSERT INTO products (
			id, tenant_id, code, name,
			version, is_active, features, meta
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, code)
		DO UPDATE SET
			name = EXCLUDED.name,
			version = EXCLUDED.version,
			is_active = EXCLUDED.is_active,
			features = EXCLUDED.features,
			meta = EXCLUDED.meta
	`

	_, err := r.db.Exec(ctx, q,
		p.ID,
		p.TenantID,
		p.Code,
		p.Name,
		p.Version,
		p.IsActive,
		p.Features,
		p.Meta,
	)
	return err
}

func (r *productRepo) SetActive(ctx context.Context, tenantID, productID string, isActive bool) error {
	const q = `
		UPDATE products
		SET is_active = $3
		WHERE tenant_id = $1 AND id = $2
	`
	tag, err := r.db.Exec(ctx, q, tenantID, productID, isActive)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrProductNotFound
	}
	return nil
}

func (r *productRepo) scanOne(ctx context.Context, q string, args ...any) (*domain.Product, error) {
	row := r.db.QueryRow(ctx, q, args...)
	p := &domain.Product{}
	if err := row.Scan(
		&p.ID,
		&p.TenantID,
		&p.Code,
		&p.Name,
		&p.Version,
		&p.IsActive,
		&p.Features,
		&p.Meta,
		&p.CreatedAt,
		&p.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrProductNotFound
		}
		return nil, fmt.Errorf("product scan: %w", err)
	}
	return p, nil
}

func (r *productRepo) scanMany(rows pgx.Rows) ([]*domain.Product, error) {
	var out []*domain.Product
	for rows.Next() {
		p := &domain.Product{}
		if err := rows.Scan(
			&p.ID,
			&p.TenantID,
			&p.Code,
			&p.Name,
			&p.Version,
			&p.IsActive,
			&p.Features,
			&p.Meta,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("product scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("products rows: %w", err)
	}
	return out, nil
}
