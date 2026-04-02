package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/devravik/go-license-api/internal/http/dto"
	lru "github.com/hashicorp/golang-lru/v2"
)

type idempotencyEntry struct {
	resp      dto.ActivateResponse
	expiresAt time.Time
}

type IdempotencyCache struct {
	lru *lru.Cache[string, idempotencyEntry]
}

func NewIdempotencyCache(maxKeys int) (*IdempotencyCache, error) {
	c, err := lru.New[string, idempotencyEntry](maxKeys)
	if err != nil {
		return nil, err
	}
	return &IdempotencyCache{lru: c}, nil
}

func (c *IdempotencyCache) Get(tenantID, key string) (dto.ActivateResponse, bool) {
	normalized := idempotencyCacheKey(tenantID, key)
	entry, ok := c.lru.Get(normalized)
	if !ok {
		return dto.ActivateResponse{}, false
	}
	if time.Now().After(entry.expiresAt) {
		c.lru.Remove(normalized)
		return dto.ActivateResponse{}, false
	}
	return entry.resp, true
}

func (c *IdempotencyCache) Set(tenantID, key string, resp dto.ActivateResponse, ttl time.Duration) {
	c.lru.Add(idempotencyCacheKey(tenantID, key), idempotencyEntry{
		resp:      resp,
		expiresAt: time.Now().Add(ttl),
	})
}

func idempotencyCacheKey(tenantID, key string) string {
	sum := sha256.Sum256([]byte(tenantID + ":" + key))
	return hex.EncodeToString(sum[:])
}
