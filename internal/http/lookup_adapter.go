package http

import (
	"context"

	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
)

// tenantKeyLookupAdapter bridges cache.TenantStore to middleware.TenantKeyLookup without DB access.
type tenantKeyLookupAdapter struct {
	store *cache.TenantStore
}

func newTenantKeyLookupAdapter(store *cache.TenantStore) *tenantKeyLookupAdapter {
	return &tenantKeyLookupAdapter{store: store}
}

func (a *tenantKeyLookupAdapter) GetByAPIKeyHash(ctx context.Context, apiKeyHash string) (*middleware.TenantKeyRecord, error) {
	t, err := a.store.GetByAPIKey(ctx, apiKeyHash)
	if err != nil || t == nil {
		return nil, err
	}
	return &middleware.TenantKeyRecord{
		TenantID: t.ID,
		Status:   t.Status,
		CIDRs:    t.IPAllowlist,
	}, nil
}
