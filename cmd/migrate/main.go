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

	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Admin helpers: migrate admin <...>
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		if err := runAdmin(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	var refresh bool
	flag.BoolVar(&refresh, "refresh", false, "drop+recreate public schema before running migrations (LOCAL ONLY)")
	flag.Parse()

	appCfg := setup.Load()
	if strings.EqualFold(appCfg.AppEnv, "production") && refresh {
		log.Fatal("refresh not allowed in production")
	}

	dbCfg := setup.LoadDatabaseConfig()
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

func runAdmin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate admin <tenant|license> <command> [flags]")
	}
	dbCfg := setup.LoadDatabaseConfig()
	databaseURL, err := dbCfg.BuildDatabaseURL()
	if err != nil {
		return fmt.Errorf("build database url: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := idb.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()
	switch args[0] {
	case "tenant":
		if len(args) < 2 {
			return fmt.Errorf("usage: migrate admin tenant <update-profile> [flags]")
		}
		switch args[1] {
		case "update-profile":
			fs := flag.NewFlagSet("update-profile", flag.ContinueOnError)
			var (
				id          string
				name        string
				slug        string
				email       string
				company     string
				plan        string
				maxLicenses int
				metadata    string
			)
			fs.StringVar(&id, "id", "", "tenant id")
			fs.StringVar(&name, "name", "", "tenant name")
			fs.StringVar(&slug, "slug", "", "tenant slug")
			fs.StringVar(&email, "email", "", "tenant email")
			fs.StringVar(&company, "company", "", "tenant company")
			fs.StringVar(&plan, "plan", "", "plan (e.g., free, pro)")
			fs.IntVar(&maxLicenses, "max-licenses", 0, "maximum licenses")
			fs.StringVar(&metadata, "metadata", "{}", "metadata JSON (string)")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if id == "" {
				return fmt.Errorf("--id is required")
			}
			md := map[string]any{}
			if strings.TrimSpace(metadata) != "" && strings.TrimSpace(metadata) != "{}" {
				md = map[string]any{"_raw": metadata}
			}
			trepo := idb.NewTenantRepo(pool)
			// Optional method via type assertion
			if tp, ok := any(trepo).(interface {
				UpdateProfile(ctx context.Context, id string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
			}); ok {
				if err := tp.UpdateProfile(ctx, id, name, slug, email, company, plan, maxLicenses, md); err != nil {
					return fmt.Errorf("update profile: %w", err)
				}
			} else {
				return fmt.Errorf("update profile not supported by repository")
			}
			fmt.Println("ok")
			return nil
		default:
			return fmt.Errorf("unknown tenant command: %s", args[1])
		}
	case "license":
		if len(args) < 2 {
			return fmt.Errorf("usage: migrate admin license <revoke> [flags]")
		}
		switch args[1] {
		case "revoke":
			fs := flag.NewFlagSet("revoke", flag.ContinueOnError)
			var (
				tenant string
				key    string
				reason string
			)
			fs.StringVar(&tenant, "tenant", "", "tenant id")
			fs.StringVar(&key, "key", "", "license key")
			fs.StringVar(&reason, "reason", "", "revocation reason")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if tenant == "" || key == "" {
				return fmt.Errorf("--tenant and --key are required")
			}
			lrepo := idb.NewLicenseRepo(pool)
			// Revoke with interface signature (reason not supported here)
			if err := lrepo.Revoke(ctx, tenant, key); err != nil {
				return fmt.Errorf("revoke: %w", err)
			}
			fmt.Println("ok")
			return nil
		default:
			return fmt.Errorf("unknown license command: %s", args[1])
		}
	default:
		return fmt.Errorf("unknown admin group: %s", args[0])
	}
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
