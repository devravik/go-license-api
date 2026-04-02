package cache

// cacheKey formats the canonical cache key.
//
// Rule: All keys MUST follow `redisPrefix + tenantID + ":" + key`.
func cacheKey(tenantID, key string) string {
	return redisPrefix + tenantID + ":" + key
}
