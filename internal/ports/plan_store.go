package ports

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

// PlanStore is an optional cache-backed store for plans.
// Validation path must remain DB-free; this store helps minimize control-plane DB reads.
type PlanStore interface {
	Get(ctx context.Context, tenantID, planID string) (*domain.Plan, error)
	Set(ctx context.Context, tenantID, planID string, plan *domain.Plan)
	Invalidate(ctx context.Context, tenantID, planID string)
}
