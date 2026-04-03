package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devravik/go-license-api/cmd/cli/loadtest"
	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/ports"
	"github.com/spf13/cobra"
)

type container struct {
	Deps *deps
}

type deps struct {
	// Filled by initDeps in di.go
	Config   *appConfig
	Services *services
}

type appConfig struct {
	Pretty  bool
	Timeout time.Duration
}

type services struct {
	// Slices required by CLI subcommands will be filled in di.go
	Admin *adminFacade
	Repo  *repoFacade
	Cache *cacheFacade
	Sys   *systemFacade
	Biz   *bizFacade
	Pub   ports.EventPublisher
}

// Root command flags
var (
	flagPretty  bool
	flagTimeout time.Duration
)

func main() {
	root := &cobra.Command{
		Use:   "cli",
		Short: "Go License Admin CLI",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return ensureDeps(cmd.Context())
		},
	}

	// Global flags
	root.PersistentFlags().BoolVar(&flagPretty, "pretty", false, "Pretty-print JSON output")
	root.PersistentFlags().DurationVar(&flagTimeout, "timeout", 10*time.Second, "Command timeout")

	// Wire subcommands in separate files
	root.AddCommand(
		newTenantCmd(),
		newLicenseCmd(),
		newProductCmd(),
		newCacheCmd(),
		newSystemCmd(),
		newLoadtestCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

var appContainer container

func ensureDeps(ctx context.Context) error {
	if appContainer.Deps != nil {
		return nil
	}
	// honor timeout and cancellation
	ctx, cancel := context.WithTimeout(withSignals(ctx), flagTimeout)
	defer cancel()
	d, err := initDeps(ctx, &appConfig{
		Pretty:  flagPretty,
		Timeout: flagTimeout,
	})
	if err != nil {
		return err
	}
	appContainer.Deps = d
	return nil
}

func withSignals(ctx context.Context) context.Context {
	cctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-cctx.Done()
		stop()
	}()
	return cctx
}

// jsonOut writes a JSON object to stdout respecting --pretty.
func jsonOut(v any) {
	enc := json.NewEncoder(os.Stdout)
	if appContainer.Deps != nil && appContainer.Deps.Config.Pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		log.Printf("encode json: %v", err)
		fmt.Println(`{"error":"encode_failed"}`)
	}
}

// jsonErr writes a JSON error and returns a cobra-friendly error.
func jsonErr(msg string, err error) error {
	if err != nil {
		jsonOut(map[string]any{"error": msg, "detail": err.Error()})
		return fmt.Errorf("%s: %w", msg, err)
	}
	jsonOut(map[string]any{"error": msg})
	return fmt.Errorf("%s", msg)
}

// newLoadtestCmd adapts root container deps to loadtest command dependencies.
// newLoadtestCmd adapts root container deps to loadtest command dependencies lazily.
func newLoadtestCmd() *cobra.Command {
	// Do not touch appContainer.Deps here; it’s nil until ensureDeps runs.
	refs := &loadtest.AppContainerRefs{}
	cmd := loadtest.NewCmd(refs)

	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Initialize global deps for this command execution
		if err := ensureDeps(cmd.Context()); err != nil {
			return err
		}
		// Now wire references safely
		refs.Timeout = appContainer.Deps.Config.Timeout
		refs.Validation = appContainer.Deps.Services.Biz.Validation
		refs.Activation = appContainer.Deps.Services.Biz.Activation
		refs.UpdateTenantProfileFn = func(ctx context.Context, tenantID, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error {
			// Prefer admin service method if available
			if u, ok := appContainer.Deps.Services.Admin.Admin.(interface {
				UpdateTenantProfile(ctx context.Context, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
			}); ok {
				return u.UpdateTenantProfile(ctx, tenantID, name, slug, email, company, plan, maxLicenses, metadata)
			}
			// Fallback to repository if supported
			if tr, ok := appContainer.Deps.Services.Repo.Tenants.(interface {
				UpdateProfile(ctx context.Context, id string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
			}); ok {
				return tr.UpdateProfile(ctx, tenantID, name, slug, email, company, plan, maxLicenses, metadata)
			}
			return fmt.Errorf("update_tenant_profile_not_supported")
		}
		refs.TenantCreate = func(ctx context.Context, rps, burst int) (*domain.Tenant, string, error) {
			return appContainer.Deps.Services.Admin.Admin.CreateTenant(ctx, rps, burst)
		}
		refs.ProductUpsert = func(ctx context.Context, p *domain.Product) error {
			return appContainer.Deps.Services.Repo.Products.Upsert(ctx, p)
		}
		refs.LicenseCreate = func(ctx context.Context, l *domain.License) error {
			return appContainer.Deps.Services.Repo.Licenses.Create(ctx, l)
		}
		refs.WriteThrough = func(ctx context.Context, tenantID, key string, lic *domain.License) {
			appContainer.Deps.Services.Cache.LicenseStore.Set(ctx, tenantID, key, lic)
		}
		// Readers for existing data
		refs.ListTenants = func(ctx context.Context, limit int) ([]loadtest.TenantInfo, error) {
			all, err := appContainer.Deps.Services.Repo.Tenants.FindAll(ctx)
			if err != nil {
				return nil, err
			}
			if limit > 0 && limit < len(all) {
				all = all[:limit]
			}
			out := make([]loadtest.TenantInfo, 0, len(all))
			for _, t := range all {
				out = append(out, loadtest.TenantInfo{ID: t.ID, APIKey: t.APIKey})
			}
			return out, nil
		}
		refs.ListLicensesByTenant = func(ctx context.Context, tenantID string, limit int) ([]loadtest.LicenseInfo, error) {
			lics, err := appContainer.Deps.Services.Repo.Licenses.ListByTenant(ctx, tenantID, max(1, limit), 0)
			if err != nil {
				return nil, err
			}
			out := make([]loadtest.LicenseInfo, 0, len(lics))
			for _, l := range lics {
				out = append(out, loadtest.LicenseInfo{Key: l.Key})
			}
			return out, nil
		}
		refs.OnTenantCreated = func(ctx context.Context, tenantID string) {
			if appContainer.Deps.Services.Pub != nil {
				_ = appContainer.Deps.Services.Pub.PublishTenantCreated(ctx, tenantID)
			}
		}
		refs.OnTenantUpdated = func(ctx context.Context, tenantID string) {
			if appContainer.Deps.Services.Pub != nil {
				_ = appContainer.Deps.Services.Pub.PublishTenantUpdated(ctx, tenantID)
			}
		}
		refs.OnProductUpserted = func(ctx context.Context, tenantID, code string) {
			if appContainer.Deps.Services.Pub != nil {
				_ = appContainer.Deps.Services.Pub.PublishProductUpserted(ctx, tenantID, code)
			}
		}
		if prev != nil {
			return prev(cmd, args)
		}
		return nil
	}

	return cmd
}
