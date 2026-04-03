package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"

	"github.com/spf13/cobra"
)

func NewCmd(app *AppContainerRefs) *cobra.Command {
	var (
		// seed flags
		tenants  int
		products int
		licenses int
		// run flags
		workers       int
		duration      time.Duration
		rps           int
		mode          string
		baseURL       string
		burst         int
		lowRPSTenants int
		coldStart     bool
		skew          string
		invalid       float64
		logging       bool
	)

	cmd := &cobra.Command{
		Use:   "loadtest",
		Short: "Load testing utilities (seed + run)",
	}

	seedCmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed tenants/products/licenses",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flag validations (pre-DB)
			if tenants <= 0 || products <= 0 || licenses <= 0 {
				return jsonErr("invalid_flags", fmt.Errorf("tenants, products, licenses must be > 0"))
			}
			// Soft cap unless explicitly large runs are desired; adjust as needed.
			if tenants > 10000 {
				return jsonErr("invalid_flags", fmt.Errorf("tenants too large"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), app.Timeout)
			defer cancel()
			arts, err := Seed(ctx, SeedConfig{
				Tenants:  tenants,
				Products: products,
				Licenses: licenses,
			}, app)
			if err != nil {
				return jsonErr("seed_failed", err)
			}
			jsonOut(map[string]any{
				"tenants":             len(arts.Tenants),
				"licenses_per_tenant": licenses,
			})
			return nil
		},
	}
	seedCmd.Flags().IntVar(&tenants, "tenants", 10, "Number of tenants")
	seedCmd.Flags().IntVar(&products, "products", 3, "Products per tenant")
	seedCmd.Flags().IntVar(&licenses, "licenses", 1000, "Licenses per tenant")
	cmd.AddCommand(seedCmd)

	var adminHTTP bool
	var adminKey string
	var doSeed bool

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run concurrent runtime simulation",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Pre-run validations (pre-IO)
			if workers <= 0 || rps <= 0 || duration <= 0 {
				return jsonErr("invalid_flags", fmt.Errorf("workers, rps, duration must be > 0"))
			}
			if mode != "http" && mode != "direct" {
				return jsonErr("invalid_flags", fmt.Errorf("mode must be http or direct"))
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			if baseURL == "" {
				baseURL = "http://localhost:8080"
			}
			// If seeding via Admin HTTP is requested, override tenant-related deps to call server.
			if adminHTTP {
				if strings.HasSuffix(baseURL, "/") {
					baseURL = strings.TrimRight(baseURL, "/")
				}
				if adminKey == "" {
					return jsonErr("missing_admin_key_for_admin_http", nil)
				}
				app.TenantCreate = func(ctx context.Context, rps, burst int) (*domain.Tenant, string, error) {
					return httpAdminCreateTenant(ctx, baseURL, adminKey, rps, burst)
				}
				app.UpdateTenantProfileFn = func(ctx context.Context, tenantID, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
					return httpAdminUpdateTenantProfile(ctx, baseURL, adminKey, tenantID, name, slug, email, company, plan, maxLicenses, metadata)
				}
			}
			var arts *SeedArtifacts
			if doSeed {
				// Explicit seeding path (opt-in)
				var err error
				arts, err = Seed(ctx, SeedConfig{
					Tenants:  tenants,
					Products: max(1, products),
					Licenses: max(1, licenses),
				}, app)
				if err != nil {
					return jsonErr("seed_failed", err)
				}
			} else {
				// Build corpus from existing data (no seeding)
				existing, err := BuildCorpusFromExisting(ctx, app, tenants, licenses)
				if err != nil {
					return jsonErr("load_existing_failed", err)
				}
				arts = existing
			}

			m := NewMetrics()
			cfg := RunConfig{
				Mode:          mode,
				BaseURL:       baseURL,
				Duration:      duration,
				Workers:       workers,
				RPS:           rps,
				Burst:         burst,
				SkewHotPct:    parseSkew(skew),
				InvalidRate:   invalid,
				OpValidate:    70,
				OpActivate:    20,
				OpUsage:       10,
				ColdStart:     coldStart,
				LowRPSTenants: lowRPSTenants,
				Logging:       logging,
			}

			corpus := &Corpus{
				Tenants:          arts.Tenants,
				LicensesByTenant: arts.LicensesByTenant,
			}
			deps := &DirectDeps{
				Validation: app.Validation,
				Activation: app.Activation,
			}
			start := time.Now()
			RunWorkers(ctx, cfg, deps, corpus, m)
			sum := m.Summarize(time.Since(start))
			printSummary(sum)
			return nil
		},
	}
	runCmd.Flags().IntVar(&tenants, "tenants", 100, "Tenants to seed (if needed)")
	runCmd.Flags().IntVar(&products, "products", 5, "Products per tenant")
	runCmd.Flags().IntVar(&licenses, "licenses", 10000, "Licenses per tenant")
	runCmd.Flags().IntVar(&workers, "workers", 200, "Concurrent workers")
	runCmd.Flags().DurationVar(&duration, "duration", 60*time.Second, "Test duration")
	runCmd.Flags().IntVar(&rps, "rps", 5000, "Target requests per second")
	runCmd.Flags().StringVar(&mode, "mode", "http", "Mode: http|direct")
	runCmd.Flags().StringVar(&baseURL, "base-url", "", "HTTP base URL (default http://localhost:8080)")
	runCmd.Flags().BoolVar(&adminHTTP, "admin-http", false, "Seed tenants via server Admin HTTP instead of direct DB")
	runCmd.Flags().StringVar(&adminKey, "admin-key", "", "Admin key for Admin HTTP (required when --admin-http)")
	runCmd.Flags().BoolVar(&doSeed, "seed", false, "Seed data before run (default false: use existing data)")
	runCmd.Flags().IntVar(&burst, "burst", 0, "Burst tokens")
	runCmd.Flags().IntVar(&lowRPSTenants, "low-rps-tenants", 0, "Number of low-RPS tenants to stress limiter")
	runCmd.Flags().BoolVar(&coldStart, "cold-start", false, "Do not prewarm caches")
	runCmd.Flags().StringVar(&skew, "skew", "80:20", "Traffic skew hot:cold percent (e.g., 80:20)")
	runCmd.Flags().Float64Var(&invalid, "invalid-rate", 0.10, "Invalid request rate (0..1)")
	runCmd.Flags().BoolVar(&logging, "logging", false, "Log curl for each HTTP request")
	cmd.AddCommand(runCmd)

	return cmd
}

// BuildCorpusFromExisting loads an existing working set from repositories via refs.
func BuildCorpusFromExisting(ctx context.Context, refs *AppContainerRefs, maxTenants, maxLicensesPerTenant int) (*SeedArtifacts, error) {
	ti, err := refs.ListTenants(ctx, maxTenants)
	if err != nil {
		return nil, err
	}
	arts := &SeedArtifacts{
		Tenants:          ti,
		LicensesByTenant: make(map[string][]LicenseInfo, len(ti)),
	}
	for _, t := range ti {
		lics, err := refs.ListLicensesByTenant(ctx, t.ID, maxLicensesPerTenant)
		if err != nil {
			return nil, err
		}
		arts.LicensesByTenant[t.ID] = lics
	}
	return arts, nil
}

// httpAdminCreateTenant calls server Admin API to create a tenant (write-through to server cache).
func httpAdminCreateTenant(ctx context.Context, baseURL, adminKey string, rps, burst int) (*domain.Tenant, string, error) {
	payload := map[string]any{"rps": rps, "burst": burst}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/admin/tenants", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", adminKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("admin_http_create_tenant: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		return nil, "", fmt.Errorf("admin_http_create_tenant_status:%d", res.StatusCode)
	}
	var out struct {
		TenantID string `json:"tenant_id"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, "", fmt.Errorf("admin_http_create_tenant_decode: %w", err)
	}
	return &domain.Tenant{ID: out.TenantID, APIKey: out.APIKey, RPS: rps, Burst: burst, Status: "active"}, out.APIKey, nil
}

// httpAdminUpdateTenantProfile updates tenant profile via server Admin API (keeps server cache consistent).
func httpAdminUpdateTenantProfile(ctx context.Context, baseURL, adminKey, tenantID, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
	payload := map[string]any{
		"name":         name,
		"slug":         slug,
		"email":        email,
		"company":      company,
		"plan":         plan,
		"max_licenses": maxLicenses,
		"metadata":     metadata,
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPatch, baseURL+"/admin/tenants/"+tenantID+"/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", adminKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin_http_update_profile: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("admin_http_update_profile_status:%d", res.StatusCode)
	}
	return nil
}

func parseSkew(s string) int {
	// format "80:20" -> returns 80
	var a int
	var b int
	_, err := fmt.Sscanf(s, "%d:%d", &a, &b)
	if err != nil || a <= 0 || b <= 0 || a+b != 100 {
		return 80
	}
	return a
}

type AppContainerRefs struct {
	Timeout               time.Duration
	Validation            app.ValidationService
	Activation            app.ActivationService
	UpdateTenantProfileFn func(ctx context.Context, tenantID, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
	TenantCreate          func(ctx context.Context, rps, burst int) (*domain.Tenant, string, error)
	ProductUpsert         func(ctx context.Context, p *domain.Product) error
	LicenseCreate         func(ctx context.Context, l *domain.License) error
	WriteThrough          func(ctx context.Context, tenantID, key string, lic *domain.License)
	// Optional event hooks
	OnTenantCreated   func(ctx context.Context, tenantID string)
	OnTenantUpdated   func(ctx context.Context, tenantID string)
	OnProductUpserted func(ctx context.Context, tenantID, code string)
	// Readers for existing data
	ListTenants          func(ctx context.Context, limit int) ([]TenantInfo, error)
	ListLicensesByTenant func(ctx context.Context, tenantID string, limit int) ([]LicenseInfo, error)
}

// CLI glue to implement SeedDeps
func (a *AppContainerRefs) CreateTenant(ctx context.Context, rps, burst int) (*domain.Tenant, string, error) {
	return a.TenantCreate(ctx, rps, burst)
}
func (a *AppContainerRefs) UpsertProduct(ctx context.Context, p *domain.Product) error {
	return a.ProductUpsert(ctx, p)
}
func (a *AppContainerRefs) CreateLicense(ctx context.Context, l *domain.License) error {
	return a.LicenseCreate(ctx, l)
}
func (a *AppContainerRefs) WriteThroughLicense(ctx context.Context, tenantID, key string, lic *domain.License) {
	a.WriteThrough(ctx, tenantID, key, lic)
}

// Implement SeedDeps.UpdateTenantProfile
func (a *AppContainerRefs) UpdateTenantProfile(ctx context.Context, tenantID, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
	if a.UpdateTenantProfileFn == nil {
		return fmt.Errorf("update_tenant_profile_not_wired")
	}
	return a.UpdateTenantProfileFn(ctx, tenantID, name, slug, email, company, plan, maxLicenses, metadata)
}

// Event forwarding (no-op if not set)
func (a *AppContainerRefs) PublishTenantCreated(ctx context.Context, tenantID string) {
	if a.OnTenantCreated != nil {
		a.OnTenantCreated(ctx, tenantID)
	}
}
func (a *AppContainerRefs) PublishTenantUpdated(ctx context.Context, tenantID string) {
	if a.OnTenantUpdated != nil {
		a.OnTenantUpdated(ctx, tenantID)
	}
}
func (a *AppContainerRefs) PublishProductUpserted(ctx context.Context, tenantID, code string) {
	if a.OnProductUpserted != nil {
		a.OnProductUpserted(ctx, tenantID, code)
	}
}

// CLI integration in root package
type jsonErrObj struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func jsonErr(msg string, err error) error {
	if err == nil {
		jsonOut(map[string]any{"error": msg})
		return fmt.Errorf("%s", msg)
	}
	jsonOut(map[string]any{"error": msg, "detail": err.Error()})
	return fmt.Errorf("%s: %v", msg, err)
}

func printSummary(s Summary) {
	jsonOut(map[string]any{
		"requests":   s.Requests,
		"success":    s.Success,
		"failures":   s.Failures,
		"avg_ms":     s.Avg.Seconds() * 1000,
		"p95_ms":     s.P95.Seconds() * 1000,
		"p99_ms":     s.P99.Seconds() * 1000,
		"throughput": s.Throughput,
		"errors":     s.Errors,
		"by_op":      s.ByOp,
	})
}

// local JSON helper to avoid depending on root package internals
func jsonOut(v any) {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(v); err != nil {
		log.Printf("encode json: %v", err)
	}
}
