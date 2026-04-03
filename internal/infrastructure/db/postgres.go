package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolLimits struct {
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// Connect creates a PostgreSQL connection pool with sensible production defaults.
// pgxpool is safe for concurrent use — share one pool across the app.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return ConnectWithLimits(ctx, databaseURL, PoolLimits{
		MaxConns:        100,
		MinConns:        2,
		MaxConnLifetime: 5 * time.Minute,
		MaxConnIdleTime: 1 * time.Minute,
	})
}

// ConnectWithLimits creates a PostgreSQL pool with explicit bounds.
func ConnectWithLimits(ctx context.Context, databaseURL string, limits PoolLimits) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	if limits.MaxConns <= 0 {
		limits.MaxConns = 100
	}
	if limits.MinConns <= 0 {
		limits.MinConns = 2
	}
	if limits.MinConns > limits.MaxConns {
		limits.MinConns = limits.MaxConns
	}
	if limits.MaxConnLifetime <= 0 {
		limits.MaxConnLifetime = 5 * time.Minute
	}
	if limits.MaxConnIdleTime <= 0 {
		limits.MaxConnIdleTime = 1 * time.Minute
	}

	cfg.MaxConns = limits.MaxConns
	cfg.MinConns = limits.MinConns
	cfg.MaxConnLifetime = limits.MaxConnLifetime
	cfg.MaxConnIdleTime = limits.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}
