package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5"
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
	const lockQ = `
		SELECT id, seat_count
		FROM licenses
		WHERE tenant_id = $1 AND key = $2 AND status = 'active'
		FOR UPDATE
	`
	if err := tx.QueryRow(ctx, lockQ, tenantID, key).Scan(&licenseID, &seatCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.ErrLicenseNotFound
		}
		return 0, fmt.Errorf("lock license: %w", err)
	}

	// Ensure we never burn a second seat for the same machine/license pair.
	var (
		existingID   string
		existingLive bool
	)
	const existingQ = `
		SELECT id, is_active
		FROM activations
		WHERE license_id = $1 AND machine_id = $2
		LIMIT 1
	`
	existingErr := tx.QueryRow(ctx, existingQ, licenseID, record.MachineID).Scan(&existingID, &existingLive)
	hasExisting := existingErr == nil
	if existingErr != nil && !errors.Is(existingErr, pgx.ErrNoRows) {
		return 0, fmt.Errorf("find activation: %w", existingErr)
	}

	// If already active, it's a no-op idempotent replay.
	if hasExisting && existingLive {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit: %w", err)
		}
		record.ID = existingID
		record.LicenseID = licenseID
		record.TenantID = tenantID
		record.IsActive = true
		return 0, nil
	}

	// Count active seats — safe because we hold the FOR UPDATE lock on the license row.
	var activeCount int
	const countQ = `SELECT COUNT(*) FROM activations WHERE license_id = $1 AND is_active = TRUE`
	if err := tx.QueryRow(ctx, countQ, licenseID).Scan(&activeCount); err != nil {
		return 0, fmt.Errorf("count seats: %w", err)
	}

	if seatCount != nil && activeCount >= *seatCount {
		return 0, domain.ErrSeatLimitReached
	}

	remaining = -1 // unlimited
	if seatCount != nil {
		remaining = *seatCount - activeCount - 1
	}

	if hasExisting && !existingLive {
		// Reactivation: reuse the existing unique (license_id, machine_id) row.
		const reactivateQ = `
			UPDATE activations
			SET
				is_active = TRUE,
				hostname = $1,
				activated_at = NOW(),
				released_at = NULL
			WHERE id = $2
		`
		if _, err := tx.Exec(ctx, reactivateQ, record.Hostname, existingID); err != nil {
			return 0, fmt.Errorf("reactivate activation: %w", err)
		}
		record.ID = existingID
	} else {
		// First activation: insert a new row.
		record.LicenseID = licenseID
		record.TenantID = tenantID
		record.IsActive = true

		const insertQ = `
			INSERT INTO activations (id, license_id, tenant_id, machine_id, hostname, is_active, activated_at)
			VALUES ($1, $2, $3, $4, $5, TRUE, NOW())
		`
		if _, err := tx.Exec(ctx, insertQ, record.ID, licenseID, tenantID, record.MachineID, record.Hostname); err != nil {
			return 0, fmt.Errorf("insert activation: %w", err)
		}
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

func (r *activationRepo) CountActive(ctx context.Context, licenseID int) (int, error) {
	const q = `SELECT COUNT(*) FROM activations WHERE license_id = $1 AND is_active = TRUE`
	var count int
	return count, r.db.QueryRow(ctx, q, licenseID).Scan(&count)
}

func (r *activationRepo) RecordUsage(ctx context.Context, licenseID, units int) error {
	// usage_records requires tenant_id, so derive it from the license row.
	const q = `
		INSERT INTO usage_records (license_id, tenant_id, units)
		SELECT $1, tenant_id, $2
		FROM licenses
		WHERE id = $1
	`

	tag, err := r.db.Exec(ctx, q, licenseID, units)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLicenseNotFound
	}
	return nil
}
