package ports

type RateLimiter interface {
	Invalidate(tenantID string)
}
