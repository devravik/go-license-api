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

type tenantRepo struct {
	db *pgxpool.Pool
}

func NewTenantRepo(db *pgxpool.Pool) domain.TenantRepository {
	return &tenantRepo{db: db}
}

func (r *tenantRepo) FindByID(ctx context.Context, id string) (*domain.Tenant, error) {
	const q = `
		SELECT
			id, api_key, COALESCE(old_api_key, '') AS old_api_key, old_key_expires_at,
			rps, burst, status,
			suspended_at, COALESCE(suspension_reason, '') AS suspension_reason, ip_allowlist,
			created_at,
			COALESCE(name, '') AS name,
			COALESCE(slug, '') AS slug,
			COALESCE(email, '') AS email,
			COALESCE(company, '') AS company,
			COALESCE(plan, '') AS plan,
			COALESCE(max_licenses, 0) AS max_licenses,
			COALESCE(metadata, '{}'::jsonb) AS metadata,
			updated_at, deleted_at
		FROM tenants
		WHERE id = $1
	`

	return r.scanTenant(r.db.QueryRow(ctx, q, id))
}

func (r *tenantRepo) FindByAPIKey(ctx context.Context, apiKey string) (*domain.Tenant, error) {
	// Supports EC-08 key rotation by matching both current api_key and old_api_key.
	const q = `
		SELECT
			id, api_key, COALESCE(old_api_key, '') AS old_api_key, old_key_expires_at,
			rps, burst, status,
			suspended_at, COALESCE(suspension_reason, '') AS suspension_reason, ip_allowlist,
			created_at,
			COALESCE(name, '') AS name,
			COALESCE(slug, '') AS slug,
			COALESCE(email, '') AS email,
			COALESCE(company, '') AS company,
			COALESCE(plan, '') AS plan,
			COALESCE(max_licenses, 0) AS max_licenses,
			COALESCE(metadata, '{}'::jsonb) AS metadata,
			updated_at, deleted_at
		FROM tenants
		WHERE api_key = $1 OR old_api_key = $1
		LIMIT 1
	`

	return r.scanTenant(r.db.QueryRow(ctx, q, apiKey))
}

func (r *tenantRepo) FindAll(ctx context.Context) ([]*domain.Tenant, error) {
	const q = `
		SELECT
			id, api_key, COALESCE(old_api_key, '') AS old_api_key, old_key_expires_at,
			rps, burst, status,
			suspended_at, COALESCE(suspension_reason, '') AS suspension_reason, ip_allowlist,
			created_at,
			COALESCE(name, '') AS name,
			COALESCE(slug, '') AS slug,
			COALESCE(email, '') AS email,
			COALESCE(company, '') AS company,
			COALESCE(plan, '') AS plan,
			COALESCE(max_licenses, 0) AS max_licenses,
			COALESCE(metadata, '{}'::jsonb) AS metadata,
			updated_at, deleted_at
		FROM tenants
	`

	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("tenants query: %w", err)
	}
	defer rows.Close()

	var out []*domain.Tenant
	for rows.Next() {
		t := &domain.Tenant{}
		if err := rows.Scan(
			&t.ID,
			&t.APIKey,
			&t.OldAPIKey,
			&t.OldKeyExpiresAt,
			&t.RPS,
			&t.Burst,
			&t.Status,
			&t.SuspendedAt,
			&t.SuspensionReason,
			&t.IPAllowlist,
			&t.CreatedAt,
			&t.Name, &t.Slug, &t.Email, &t.Company, &t.Plan, &t.MaxLicenses, &t.Metadata, &t.UpdatedAt, &t.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("tenant scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenants rows: %w", err)
	}

	return out, nil
}

func (r *tenantRepo) Create(ctx context.Context, t *domain.Tenant) error {
	const q = `
		INSERT INTO tenants (id, api_key, rps, burst, status, name, slug, email, company, plan, max_licenses, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := r.db.Exec(ctx, q, t.ID, t.APIKey, t.RPS, t.Burst, t.Status, t.Name, t.Slug, t.Email, t.Company, t.Plan, t.MaxLicenses, t.Metadata)
	return err
}

func (r *tenantRepo) UpdateStatus(ctx context.Context, id, status string) error {
	const q = `
		UPDATE tenants
		SET status = $1,
			suspended_at = CASE WHEN $1 IN ('suspended','deleted') THEN NOW() ELSE NULL END
		WHERE id = $2
	`
	_, err := r.db.Exec(ctx, q, status, id)
	return err
}

func (r *tenantRepo) UpdateLimits(ctx context.Context, id string, rps, burst int) error {
	const q = `
		UPDATE tenants
		SET rps = $1, burst = $2
		WHERE id = $3
	`
	_, err := r.db.Exec(ctx, q, rps, burst, id)
	return err
}

func (r *tenantRepo) UpdateIPAllowlist(ctx context.Context, id string, cidrs []string) error {
	const q = `UPDATE tenants SET ip_allowlist = $1 WHERE id = $2`
	_, err := r.db.Exec(ctx, q, cidrs, id)
	return err
}

func (r *tenantRepo) RotateAPIKey(ctx context.Context, id, newKey string, gracePeriod time.Duration) error {
	const q = `
		UPDATE tenants
		SET
			old_api_key = api_key,
			old_key_expires_at = NOW() + ($3 * INTERVAL '1 second'),
			api_key = $2
		WHERE id = $1
	`

	seconds := int64(gracePeriod / time.Second)
	_, err := r.db.Exec(ctx, q, id, newKey, seconds)
	return err
}

func (r *tenantRepo) scanTenant(row pgx.Row) (*domain.Tenant, error) {
	t := &domain.Tenant{}
	err := row.Scan(
		&t.ID,
		&t.APIKey,
		&t.OldAPIKey,
		&t.OldKeyExpiresAt,
		&t.RPS,
		&t.Burst,
		&t.Status,
		&t.SuspendedAt,
		&t.SuspensionReason,
		&t.IPAllowlist,
		&t.CreatedAt,
		&t.Name, &t.Slug, &t.Email, &t.Company, &t.Plan, &t.MaxLicenses, &t.Metadata, &t.UpdatedAt, &t.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidTenant
		}
		return nil, fmt.Errorf("tenant scan: %w", err)
	}
	return t, nil
}

// Optional helper not part of the TenantRepository interface.
func (r *tenantRepo) FindBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	const q = `
		SELECT
			id, api_key, COALESCE(old_api_key, '') AS old_api_key, old_key_expires_at,
			rps, burst, status,
			suspended_at, COALESCE(suspension_reason, '') AS suspension_reason, ip_allowlist,
			created_at,
			COALESCE(name, '') AS name,
			COALESCE(slug, '') AS slug,
			COALESCE(email, '') AS email,
			COALESCE(company, '') AS company,
			COALESCE(plan, '') AS plan,
			COALESCE(max_licenses, 0) AS max_licenses,
			COALESCE(metadata, '{}'::jsonb) AS metadata,
			updated_at, deleted_at
		FROM tenants
		WHERE slug = $1
		LIMIT 1
	`
	return r.scanTenant(r.db.QueryRow(ctx, q, slug))
}

// Optional helper not part of the TenantRepository interface.
func (r *tenantRepo) UpdateProfile(ctx context.Context, id string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
	const q = `
		UPDATE tenants
		SET
			name = $2,
			slug = $3,
			email = $4,
			company = $5,
			plan = $6,
			max_licenses = $7,
			metadata = $8
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, q, id, name, slug, email, company, plan, maxLicenses, metadata)
	return err
}
