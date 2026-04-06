package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
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
			id, tenant_id, type, plan_id, product_id, key, status, not_before, expires_at,
			seats_total, seats_used, features,
			overrides_features_add, overrides_features_remove,
			trial_enabled, trial_ends_at, trial_features,
			metadata, created_at, updated_at, revocation_id
		FROM licenses
		WHERE tenant_id = $1 AND key = $2 AND deleted_at IS NULL
		LIMIT 1
	`

	l := &domain.License{}
	row := r.db.QueryRow(ctx, q, tenantID, key)
	if err := row.Scan(
		&l.ID,
		&l.TenantID,
		&l.Type,
		&l.PlanID,
		&l.ProductID,
		&l.Key,
		&l.Status,
		&l.NotBefore,
		&l.ExpiresAt,
		&l.SeatsTotal,
		&l.SeatsUsed,
		&l.Features,
		&l.Overrides.FeaturesAdd,
		&l.Overrides.FeaturesRemove,
		&l.Trial.Enabled,
		&l.Trial.EndsAt,
		&l.Trial.Features,
		&l.Metadata,
		&l.CreatedAt,
		&l.UpdatedAt,
		&l.RevocationID,
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
			id, tenant_id, type, plan_id, product_id, key, status, not_before, expires_at,
			seats_total, seats_used, features,
			overrides_features_add, overrides_features_remove,
			trial_enabled, trial_ends_at, trial_features, metadata, revocation_id
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			$10,$11,$12,
			$13,$14,
			$15,$16,$17,$18,$19
		)
		RETURNING id
	`
	if l.ID == "" {
		id, err := idgen.NewID("lic")
		if err != nil {
			return fmt.Errorf("generate license id: %w", err)
		}
		l.ID = id
	}
	if l.RevocationID == "" {
		rid, err := idgen.NewID("rev")
		if err != nil {
			return fmt.Errorf("generate revocation id: %w", err)
		}
		l.RevocationID = rid
	}

	return r.db.QueryRow(ctx, q,
		l.ID, l.TenantID, l.Type, l.PlanID, l.ProductID, l.Key, l.Status, l.NotBefore, l.ExpiresAt,
		l.SeatsTotal, l.SeatsUsed, l.Features,
		l.Overrides.FeaturesAdd, l.Overrides.FeaturesRemove,
		l.Trial.Enabled, l.Trial.EndsAt, l.Trial.Features, l.Metadata, l.RevocationID,
	).Scan(&l.ID)
}

func (r *licenseRepo) Revoke(ctx context.Context, tenantID, key string) error {
	const q = `
		UPDATE licenses
		SET status = 'revoked',
			revoked_at = NOW(),
			revoked_reason = revoked_reason
		WHERE tenant_id = $1 AND key = $2 AND deleted_at IS NULL
	`
	_, err := r.db.Exec(ctx, q, tenantID, key)
	return err
}

func (r *licenseRepo) Update(ctx context.Context, l *domain.License) error {
	const q = `
		UPDATE licenses SET
			type = $3,
			plan_id = $4,
			product_id = $5,
			status = $6,
			not_before = $7,
			expires_at = $8,
			seats_total = $9,
			seats_used = $10,
			features = $11,
			overrides_features_add = $12,
			overrides_features_remove = $13,
			trial_enabled = $14,
			trial_ends_at = $15,
			trial_features = $16,
			metadata = $17,
			revocation_id = COALESCE($18, revocation_id)
		WHERE tenant_id = $1 AND key = $2 AND deleted_at IS NULL
	`
	_, err := r.db.Exec(ctx, q,
		l.TenantID, l.Key,
		l.Type, l.PlanID, l.ProductID,
		l.Status, l.NotBefore, l.ExpiresAt, l.SeatsTotal, l.SeatsUsed,
		l.Features, l.Overrides.FeaturesAdd, l.Overrides.FeaturesRemove,
		l.Trial.Enabled, l.Trial.EndsAt, l.Trial.Features, l.Metadata, l.RevocationID,
	)
	return err
}

func (r *licenseRepo) ListByTenant(ctx context.Context, tenantID string, limit, offset int) ([]*domain.License, error) {
	const q = `
		SELECT
			id, tenant_id, type, plan_id, product_id, key, status, not_before, expires_at,
			seats_total, seats_used, features,
			overrides_features_add, overrides_features_remove,
			trial_enabled, trial_ends_at, trial_features,
			metadata, created_at, updated_at, revocation_id
		FROM licenses
		WHERE tenant_id = $1 AND deleted_at IS NULL
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
			&l.Type,
			&l.PlanID,
			&l.ProductID,
			&l.Key,
			&l.Status,
			&l.NotBefore,
			&l.ExpiresAt,
			&l.SeatsTotal,
			&l.SeatsUsed,
			&l.Features,
			&l.Overrides.FeaturesAdd,
			&l.Overrides.FeaturesRemove,
			&l.Trial.Enabled,
			&l.Trial.EndsAt,
			&l.Trial.Features,
			&l.Metadata,
			&l.CreatedAt,
			&l.UpdatedAt,
			&l.RevocationID,
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
			id, tenant_id, type, plan_id, product_id, key, status, not_before, expires_at,
			seats_total, seats_used, features,
			overrides_features_add, overrides_features_remove,
			trial_enabled, trial_ends_at, trial_features,
			metadata, created_at, updated_at, revocation_id
		FROM licenses
		WHERE deleted_at IS NULL
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
			&l.Type,
			&l.PlanID,
			&l.ProductID,
			&l.Key,
			&l.Status,
			&l.NotBefore,
			&l.ExpiresAt,
			&l.SeatsTotal,
			&l.SeatsUsed,
			&l.Features,
			&l.Overrides.FeaturesAdd,
			&l.Overrides.FeaturesRemove,
			&l.Trial.Enabled,
			&l.Trial.EndsAt,
			&l.Trial.Features,
			&l.Metadata,
			&l.CreatedAt,
			&l.UpdatedAt,
			&l.RevocationID,
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

// Optional helper not part of the interface.
func (r *licenseRepo) UpdateLastValidatedAt(ctx context.Context, tenantID, key string, at time.Time) error {
	const q = `UPDATE licenses SET last_validated_at = $3 WHERE tenant_id = $1 AND key = $2 AND deleted_at IS NULL`
	_, err := r.db.Exec(ctx, q, tenantID, key, at)
	return err
}

func (r *licenseRepo) ListRevocationsSince(ctx context.Context, since *time.Time, limit int) ([]domain.Revocation, error) {
	var rows pgx.Rows
	var err error
	if since != nil {
		const q = `
			SELECT revocation_id, id, COALESCE(revoked_at, updated_at) AS revoked_at, COALESCE(revoked_reason, '') AS reason
			FROM licenses
			WHERE status = 'revoked' AND COALESCE(revoked_at, updated_at) >= $1 AND deleted_at IS NULL
			ORDER BY COALESCE(revoked_at, updated_at) ASC
			LIMIT $2
		`
		rows, err = r.db.Query(ctx, q, *since, limit)
	} else {
		const q = `
			SELECT revocation_id, id, COALESCE(revoked_at, updated_at) AS revoked_at, COALESCE(revoked_reason, '') AS reason
			FROM licenses
			WHERE status = 'revoked' AND deleted_at IS NULL
			ORDER BY COALESCE(revoked_at, updated_at) ASC
			LIMIT $1
		`
		rows, err = r.db.Query(ctx, q, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Revocation, 0, 128)
	for rows.Next() {
		var rec domain.Revocation
		if err := rows.Scan(&rec.RevocationID, &rec.LicenseID, &rec.RevokedAt, &rec.Reason); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
