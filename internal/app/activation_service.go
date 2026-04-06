package app

import (
	"context"
	"log"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
	"github.com/devravik/go-license-api/internal/ports"
)

type ActivationService interface {
	Activate(ctx context.Context, tenantID, key, clientID, hostname string) (*domain.ActivationRecord, int, int, error)
	Deactivate(ctx context.Context, tenantID, key, clientID string) error
	RecordUsage(ctx context.Context, tenantID, key string, units int) (totalUsed int, remaining *int, err error)
	GetActiveByClient(ctx context.Context, tenantID, key, clientID string) (*domain.ActivationRecord, error)
}

type ActivationLocker interface {
	Lock(licenseID string)
	Unlock(licenseID string)
}

type ActivationLicenseStore interface {
	Get(ctx context.Context, tenantID, key string) (*domain.License, error)
	Invalidate(ctx context.Context, tenantID, key string) error
}

type activationService struct {
	store       ActivationLicenseStore
	repo        domain.LicenseRepository
	cacheWriter ports.LicenseCacheWriter
	activations domain.ActivationRepository
	auditor     domain.AuditWriter
	locker      ActivationLocker
}

func NewActivationService(
	store ActivationLicenseStore,
	repo domain.LicenseRepository,
	cacheWriter ports.LicenseCacheWriter,
	activations domain.ActivationRepository,
	auditor domain.AuditWriter,
	locker ActivationLocker,
) ActivationService {
	return &activationService{
		store:       store,
		repo:        repo,
		cacheWriter: cacheWriter,
		activations: activations,
		auditor:     auditor,
		locker:      locker,
	}
}

func (s *activationService) Activate(ctx context.Context, tenantID, key, clientID, hostname string) (*domain.ActivationRecord, int, int, error) {
	license, err := s.resolveLicense(ctx, tenantID, key)
	if err != nil {
		return nil, 0, 0, err
	}
	if license.IsRevoked() {
		return nil, 0, 0, domain.ErrLicenseRevoked
	}
	if license.IsExpired() {
		return nil, 0, 0, domain.ErrLicenseExpired
	}
	if license.IsInGracePeriod() {
		return nil, 0, 0, domain.ErrLicenseGracePeriod
	}
	if license.SeatsTotal != -1 && license.SeatsUsed >= license.SeatsTotal {
		return nil, 0, 0, domain.ErrSeatLimitReached
	}

	s.locker.Lock(license.ID)
	defer s.locker.Unlock(license.ID)

	record := &domain.ActivationRecord{
		TenantID:    tenantID,
		ClientID:    clientID,
		Hostname:    hostname,
		IsActive:    true,
		ActivatedAt: time.Now(),
	}
	recordID, err := idgen.NewID("act")
	if err != nil {
		return nil, 0, 0, err
	}
	record.ID = recordID

	writeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	remaining, err := s.activations.ActivateWithLock(writeCtx, tenantID, key, record)
	if err != nil {
		return nil, 0, 0, err
	}

	if err := s.store.Invalidate(ctx, tenantID, key); err != nil {
		log.Printf("cache invalidate failed op=activate tenant=%s key=%s err=%v", tenantID, key, err)
	}

	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventLicenseActivated,
		ResourceID: key,
		Outcome:    "success",
		Meta: map[string]any{
			"client_id": clientID,
			"hostname":  hostname,
		},
	})

	totalSeats := -1
	if license.SeatsTotal != 0 {
		totalSeats = license.SeatsTotal
	} else if license.SeatCount != nil {
		totalSeats = *license.SeatCount
	}
	return record, remaining, totalSeats, nil
}

func (s *activationService) Deactivate(ctx context.Context, tenantID, key, clientID string) error {
	license, err := s.resolveLicense(ctx, tenantID, key)
	if err != nil {
		return err
	}

	s.locker.Lock(license.ID)
	defer s.locker.Unlock(license.ID)

	if err := s.activations.ReleaseByClient(ctx, tenantID, key, clientID); err != nil {
		return err
	}

	if err := s.store.Invalidate(ctx, tenantID, key); err != nil {
		log.Printf("cache invalidate failed op=deactivate tenant=%s key=%s err=%v", tenantID, key, err)
	}

	s.auditor.Write(ctx, &domain.AuditEntry{
		TenantID:   tenantID,
		Event:      domain.EventLicenseDeactivated,
		ResourceID: key,
		Outcome:    "success",
	})
	return nil
}

func (s *activationService) RecordUsage(ctx context.Context, tenantID, key string, units int) (int, *int, error) {
	license, err := s.resolveLicense(ctx, tenantID, key)
	if err != nil {
		return 0, nil, err
	}
	if license.IsRevoked() {
		return 0, nil, domain.ErrLicenseRevoked
	}
	if license.IsExpired() && !license.IsInGracePeriod() {
		return 0, nil, domain.ErrLicenseExpired
	}
	total, limit, err := s.activations.RecordUsage(ctx, license.ID, units)
	if err != nil {
		return 0, nil, err
	}
	if limit == nil {
		return total, nil, nil
	}
	rem := *limit - total
	return total, &rem, nil
}

func (s *activationService) GetActiveByClient(ctx context.Context, tenantID, key, clientID string) (*domain.ActivationRecord, error) {
	if s.activations == nil {
		return nil, domain.ErrLicenseNotFound
	}
	return s.activations.FindActiveByClient(ctx, tenantID, key, clientID)
}

func (s *activationService) resolveLicense(ctx context.Context, tenantID, key string) (*domain.License, error) {
	license, err := s.store.Get(ctx, tenantID, key)
	if err == nil && license != nil {
		return license, nil
	}
	if s.repo == nil {
		return nil, err
	}
	dbLic, derr := s.repo.FindByKey(ctx, tenantID, key)
	if derr != nil || dbLic == nil {
		if derr != nil {
			return nil, derr
		}
		return nil, domain.ErrLicenseNotFound
	}
	if s.cacheWriter != nil {
		s.cacheWriter.Set(ctx, tenantID, key, dbLic)
	}
	return dbLic, nil
}
