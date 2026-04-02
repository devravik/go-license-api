package app

import (
	"context"
	"log"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/google/uuid"
)

type ActivationService interface {
	Activate(ctx context.Context, tenantID, key, machineID, hostname string) (*domain.ActivationRecord, int, error)
	Deactivate(ctx context.Context, tenantID, key, activationID string) error
	RecordUsage(ctx context.Context, tenantID string, licenseID, units int) error
}

type ActivationLocker interface {
	Lock(licenseID int)
	Unlock(licenseID int)
}

type ActivationLicenseStore interface {
	Get(ctx context.Context, tenantID, key string) (*domain.License, error)
	Invalidate(ctx context.Context, tenantID, key string) error
}

type activationService struct {
	store       ActivationLicenseStore
	activations domain.ActivationRepository
	auditor     domain.AuditWriter
	locker      ActivationLocker
}

func NewActivationService(
	store ActivationLicenseStore,
	activations domain.ActivationRepository,
	auditor domain.AuditWriter,
	locker ActivationLocker,
) ActivationService {
	return &activationService{
		store:       store,
		activations: activations,
		auditor:     auditor,
		locker:      locker,
	}
}

func (s *activationService) Activate(ctx context.Context, tenantID, key, machineID, hostname string) (*domain.ActivationRecord, int, error) {
	license, err := s.store.Get(ctx, tenantID, key)
	if err != nil {
		return nil, 0, err
	}
	if license.IsRevoked() {
		return nil, 0, domain.ErrLicenseRevoked
	}
	if license.IsExpired() {
		return nil, 0, domain.ErrLicenseExpired
	}
	if license.IsInGracePeriod() {
		return nil, 0, domain.ErrLicenseGracePeriod
	}

	s.locker.Lock(license.ID)
	defer s.locker.Unlock(license.ID)

	record := &domain.ActivationRecord{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		MachineID:   machineID,
		Hostname:    hostname,
		IsActive:    true,
		ActivatedAt: time.Now(),
	}

	writeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	remaining, err := s.activations.ActivateWithLock(writeCtx, tenantID, key, record)
	if err != nil {
		return nil, 0, err
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
			"machine_id": machineID,
			"hostname":   hostname,
		},
	})

	return record, remaining, nil
}

func (s *activationService) Deactivate(ctx context.Context, tenantID, key, activationID string) error {
	license, err := s.store.Get(ctx, tenantID, key)
	if err != nil {
		return err
	}

	s.locker.Lock(license.ID)
	defer s.locker.Unlock(license.ID)

	if err := s.activations.Release(ctx, activationID); err != nil {
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

func (s *activationService) RecordUsage(ctx context.Context, _ string, licenseID, units int) error {
	return s.activations.RecordUsage(ctx, licenseID, units)
}
