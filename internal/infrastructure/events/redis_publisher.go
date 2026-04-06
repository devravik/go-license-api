package events

import (
	"context"

	"github.com/redis/go-redis/v9"
)

type RedisEventPublisher struct {
	client *redis.Client
}

func NewRedisEventPublisher(redisURL string) (*RedisEventPublisher, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &RedisEventPublisher{client: redis.NewClient(opt)}, nil
}

func (p *RedisEventPublisher) PublishTenantCreated(ctx context.Context, tenantID string) error {
	return p.client.Publish(ctx, "tenant:created", tenantID).Err()
}

func (p *RedisEventPublisher) PublishTenantUpdated(ctx context.Context, tenantID string) error {
	return p.client.Publish(ctx, "tenant:updated", tenantID).Err()
}

func (p *RedisEventPublisher) PublishProductUpserted(ctx context.Context, tenantID, code string) error {
	return p.client.Publish(ctx, "product:upsert", tenantID+"|"+code).Err()
}

func (p *RedisEventPublisher) PublishProductDeleted(ctx context.Context, tenantID, code string) error {
	return p.client.Publish(ctx, "product:delete", tenantID+"|"+code).Err()
}

type NoopPublisher struct{}

func (NoopPublisher) PublishTenantCreated(ctx context.Context, tenantID string) error { return nil }
func (NoopPublisher) PublishTenantUpdated(ctx context.Context, tenantID string) error { return nil }
func (NoopPublisher) PublishProductUpserted(ctx context.Context, tenantID, code string) error {
	return nil
}
func (NoopPublisher) PublishProductDeleted(ctx context.Context, tenantID, code string) error {
	return nil
}
