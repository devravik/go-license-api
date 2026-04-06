package ports

import "context"

// EventPublisher broadcasts control-plane domain events to other processes.
// Implementations may be Redis Pub/Sub, NATS, or no-op.
type EventPublisher interface {
	PublishTenantCreated(ctx context.Context, tenantID string) error
	PublishTenantUpdated(ctx context.Context, tenantID string) error
	PublishProductUpserted(ctx context.Context, tenantID, code string) error
	PublishProductDeleted(ctx context.Context, tenantID, code string) error
}
