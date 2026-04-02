//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	idb "github.com/devravik/go-license-api/internal/infrastructure/db"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ensureMigrationsApplied(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	const qSchemaMigrations = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := pool.Exec(ctx, qSchemaMigrations); err != nil {
		return err
	}

	// Load applied versions.
	const qApplied = `SELECT version FROM schema_migrations`
	rows, err := pool.Query(ctx, qApplied)
	if err != nil {
		return err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
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
		files = append(files, filepath.Join(migrationsDir, name))
	}
	sort.Strings(files)

	for _, path := range files {
		base := filepath.Base(path)
		version, err := versionFromFilename(base)
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sql := strings.TrimSpace(string(b))
		if sql == "" {
			continue
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sql); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
}

func versionFromFilename(name string) (string, error) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 2 || parts[0] == "" {
		return "", fmt.Errorf("unexpected migration filename %q", name)
	}
	return parts[0], nil
}

func TestLicenseRepo_FindByKey_CRUDAndIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL (or DATABASE_URL) to run integration tests")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	// Ensure schema is up to date for the integration run.
	migrationsDir := filepath.Join(repoRoot(t), "migrations")
	if err := ensureMigrationsApplied(ctx, pool, migrationsDir); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	tenantRepo := idb.NewTenantRepo(pool)
	licenseRepo := idb.NewLicenseRepo(pool)

	tenantID := fmt.Sprintf("test_tenant_%d", time.Now().UnixNano())
	apiKey := fmt.Sprintf("tenant_key_%d", time.Now().UnixNano())
	licenseKey := "INTEG-001"
	otherTenantID := fmt.Sprintf("test_tenant_%d", time.Now().UnixNano()+1)

	// Seed tenant row.
	if err := tenantRepo.Create(ctx, &domain.Tenant{
		ID:     tenantID,
		APIKey: apiKey,
		RPS:    100,
		Burst:  200,
		Status: "active",
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	expired := time.Now().Add(24 * time.Hour)
	lic := &domain.License{
		TenantID:        tenantID,
		Key:             licenseKey,
		Product:         "pro",
		ProductID:       "product_1",
		Status:          "active",
		Plan:            "starter",
		IsTrial:         false,
		ExpiresAt:       &expired,
		GracePeriodDays: 0,
		Features:        []string{"sso"},
		Meta:            map[string]any{"plan": "starter"},
	}

	if err := licenseRepo.Create(ctx, lic); err != nil {
		t.Fatalf("create license: %v", err)
	}

	found, err := licenseRepo.FindByKey(ctx, tenantID, licenseKey)
	if err != nil {
		t.Fatalf("find license: %v", err)
	}
	if found.Key != licenseKey || found.TenantID != tenantID || found.Plan != "starter" {
		t.Fatalf("unexpected license: %+v", found)
	}

	// Revoke and verify state change.
	if err := licenseRepo.Revoke(ctx, tenantID, licenseKey); err != nil {
		t.Fatalf("revoke license: %v", err)
	}

	found2, err := licenseRepo.FindByKey(ctx, tenantID, licenseKey)
	if err != nil {
		t.Fatalf("find revoked license: %v", err)
	}
	if found2.Status != "revoked" {
		t.Fatalf("expected status revoked, got %s", found2.Status)
	}

	// Tenant isolation: same key for different tenant should look absent.
	_, err = licenseRepo.FindByKey(ctx, otherTenantID, licenseKey)
	if err == nil || err != domain.ErrLicenseNotFound {
		t.Fatalf("expected ErrLicenseNotFound for other tenant, got %v", err)
	}

	// Duplicate unique key: (tenant_id, key) must fail.
	dup := &domain.License{
		TenantID:        tenantID,
		Key:             licenseKey,
		Product:         "pro",
		ProductID:       "product_1",
		Status:          "active",
		Plan:            "starter",
		IsTrial:         false,
		ExpiresAt:       &expired,
		GracePeriodDays: 0,
		Features:        []string{"sso"},
		Meta:            map[string]any{"plan": "starter"},
	}
	if err := licenseRepo.Create(ctx, dup); err == nil {
		t.Fatalf("expected duplicate create to fail")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// Derive repo root from test file location.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get caller")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

