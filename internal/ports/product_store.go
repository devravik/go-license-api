package ports

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
)

// ProductStore is an optional cache-backed store for products.
// Validation path must remain DB-free; use this when product attributes are needed at runtime.
type ProductStore interface {
	Get(ctx context.Context, tenantID, code string) (*domain.Product, error)
	Set(ctx context.Context, tenantID, code string, product *domain.Product)
	Invalidate(ctx context.Context, tenantID, code string)
}
