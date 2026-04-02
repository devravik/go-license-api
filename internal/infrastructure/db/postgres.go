package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a PostgreSQL connection pool with sensible production defaults.
// pgxpool is safe for concurrent use — share one pool across the app.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	// Tune pool size: enough for worker pool + admin handlers + health checks.
	// If MaxConns isn't explicitly set, default to something reasonable for dev/prod.
	if cfg.MaxConns == 0 || cfg.MaxConns == 4 {
		cfg.MaxConns = 20
	}

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
