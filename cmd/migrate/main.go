package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devravik/go-license-api/configs"
	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	var refresh bool
	flag.BoolVar(&refresh, "refresh", false, "drop+recreate public schema before running migrations (LOCAL ONLY)")
	flag.Parse()

	appCfg := configs.Load()
	if strings.EqualFold(appCfg.AppEnv, "production") && refresh {
		log.Fatal("refresh not allowed in production")
	}

	dbCfg := configs.LoadDatabaseConfig()
	databaseURL, err := dbCfg.BuildDatabaseURL()
	if err != nil {
		log.Fatalf("build database url: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := idb.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if refresh {
		if err := refreshPublicSchema(ctx, pool); err != nil {
			log.Fatalf("refresh schema: %v", err)
		}
	}

	if err := ensureSchemaMigrations(ctx, pool); err != nil {
		log.Fatalf("ensure schema_migrations: %v", err)
	}

	applied, err := loadAppliedVersions(ctx, pool)
	if err != nil {
		log.Fatalf("load applied migrations: %v", err)
	}

	files, err := listMigrationFiles("migrations")
	if err != nil {
		log.Fatalf("list migrations: %v", err)
	}
	if len(files) == 0 {
		log.Println("no migration files found")
		return
	}

	for _, path := range files {
		version, err := versionFromFilename(filepath.Base(path))
		if err != nil {
			log.Fatalf("invalid migration filename %s: %v", path, err)
		}

		if applied[version] {
			log.Printf("skipping %s (already applied)\n", path)
			continue
		}

		if err := applyMigration(ctx, pool, version, path); err != nil {
			log.Fatalf("apply %s: %v", path, err)
		}

		log.Printf("applied %s\n", path)
	}

	log.Println("migrations complete")
}

func ensureSchemaMigrations(ctx context.Context, pool execer) error {
	const q = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := pool.Exec(ctx, q)
	return err
}

func loadAppliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	const q = `SELECT version FROM schema_migrations`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, version, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sql := strings.TrimSpace(string(b))
	if sql == "" {
		return fmt.Errorf("empty migration file")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func listMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}

	sort.Strings(files) // 001_*.sql, 002_*.sql, ...
	return files, nil
}

func versionFromFilename(name string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(name), ".sql") {
		return "", fmt.Errorf("must end with .sql")
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))

	// Expect: 001_init, 002_products, etc.
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 2 {
		return "", fmt.Errorf("expected <version>_<name>.sql")
	}
	v := parts[0]
	if v == "" {
		return "", fmt.Errorf("missing version prefix")
	}
	return v, nil
}

func refreshPublicSchema(ctx context.Context, pool execer) error {
	const q = `
		DROP SCHEMA IF EXISTS public CASCADE;
		CREATE SCHEMA public;
		GRANT ALL ON SCHEMA public TO public;
	`
	_, err := pool.Exec(ctx, q)
	return err
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

var _ = errors.New
