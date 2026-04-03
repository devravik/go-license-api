package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type activationRepo struct {
	db *pgxpool.Pool
}

func NewActivationRepo(db *pgxpool.Pool) domain.ActivationRepository {
	return &activationRepo{db: db}
}

func (r *activationRepo) ActivateWithLock(ctx context.Context, tenantID, key string, record *domain.ActivationRecord) (remaining int, err error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// Lock the license row for the duration of this transaction.
	var licenseID int
	var seatCount *int
	var status string
	var expiresAt *time.Time
	const lockQ = `
		SELECT id, seat_count, status, expires_at
		FROM licenses
		WHERE tenant_id = $1 AND key = $2
		FOR UPDATE
	`
	if err := tx.QueryRow(ctx, lockQ, tenantID, key).Scan(&licenseID, &seatCount, &status, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.ErrLicenseNotFound
		}
		return 0, fmt.Errorf("lock license: %w", err)
	}

	if status != "active" {
		return 0, domain.ErrLicenseRevoked
	}
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return 0, domain.ErrLicenseExpired
	}

	// Aggregate active count and duplicate active activation in one query.
	const usageQ = `
		SELECT
			COUNT(*) FILTER (WHERE is_active = TRUE) AS active_count,
			MAX(CASE WHEN machine_id = $2 AND is_active THEN id END) AS existing_id
		FROM activations
		WHERE license_id = $1
	`
	var activeCount int
	var existingID *string
	if err := tx.QueryRow(ctx, usageQ, licenseID, record.MachineID).Scan(&activeCount, &existingID); err != nil {
		return 0, fmt.Errorf("activation usage stats: %w", err)
	}
	if existingID != nil {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit: %w", err)
		}
		record.ID = *existingID
		record.LicenseID = licenseID
		record.TenantID = tenantID
		record.IsActive = true
		if seatCount == nil {
			return -1, nil
		}
		return *seatCount - activeCount, nil
	}

	if seatCount != nil && activeCount >= *seatCount {
		return 0, domain.ErrSeatLimitReached
	}

	remaining = -1 // unlimited
	if seatCount != nil {
		remaining = *seatCount - activeCount - 1
	}

	record.LicenseID = licenseID
	record.TenantID = tenantID
	record.IsActive = true

	const insertQ = `
		INSERT INTO activations (id, license_id, tenant_id, machine_id, hostname, is_active, activated_at, ip, user_agent, metadata)
		VALUES ($1, $2, $3, $4, $5, TRUE, NOW(), $6, $7, $8)
	`
	if _, err := tx.Exec(ctx, insertQ, record.ID, licenseID, tenantID, record.MachineID, record.Hostname, record.IP, record.UserAgent, record.Metadata); err != nil {
		// Insertion can race. If unique index fires, return the existing active row.
		if isUniqueViolation(err) {
			const existingQ = `
				SELECT id
				FROM activations
				WHERE license_id = $1 AND machine_id = $2 AND is_active = TRUE
				LIMIT 1
			`
			var uniqID string
			if qErr := tx.QueryRow(ctx, existingQ, licenseID, record.MachineID).Scan(&uniqID); qErr == nil {
				if cErr := tx.Commit(ctx); cErr != nil {
					return 0, fmt.Errorf("commit after unique hit: %w", cErr)
				}
				record.ID = uniqID
				return 0, nil
			}
		}
		return 0, fmt.Errorf("insert activation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	record.LicenseID = licenseID
	record.TenantID = tenantID
	record.IsActive = true

	return remaining, nil
}

func (r *activationRepo) Release(ctx context.Context, activationID string) error {
	const q = `UPDATE activations SET is_active = FALSE, released_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, q, activationID)
	return err
}

func (r *activationRepo) ReleaseByMachine(ctx context.Context, tenantID, key, machineID string) error {
	const q = `
		UPDATE activations a
		SET is_active = FALSE, released_at = NOW()
		FROM licenses l
		WHERE a.license_id = l.id
		  AND l.tenant_id = $1
		  AND l.key = $2
		  AND a.machine_id = $3
		  AND a.is_active = TRUE
	`
	tag, err := r.db.Exec(ctx, q, tenantID, key, machineID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLicenseNotFound
	}
	return nil
}

func (r *activationRepo) CountActive(ctx context.Context, licenseID int) (int, error) {
	const q = `SELECT COUNT(*) FROM activations WHERE license_id = $1 AND is_active = TRUE`
	var count int
	return count, r.db.QueryRow(ctx, q, licenseID).Scan(&count)
}

func (r *activationRepo) RecordUsage(ctx context.Context, licenseID, units int) (int, *int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	// Update running total on licenses for fast retrieval and compute remaining using usage_limit.
	var totalUsed int
	var limit *int
	const updQ = `
		UPDATE licenses
		SET usage_used = COALESCE(usage_used, 0) + $2
		WHERE id = $1
		RETURNING usage_used, usage_limit
	`
	if err := tx.QueryRow(ctx, updQ, licenseID, units).Scan(&totalUsed, &limit); err != nil {
		return 0, nil, err
	}
	// Insert detailed record (tenant_id derived).
	const insQ = `
		INSERT INTO usage_records (license_id, tenant_id, units)
		SELECT $1, tenant_id, $2
		FROM licenses
		WHERE id = $1
	`
	if _, err := tx.Exec(ctx, insQ, licenseID, units); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return totalUsed, limit, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
