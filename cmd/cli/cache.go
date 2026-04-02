package main

import (
	"context"

	"github.com/spf13/cobra"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Cache/runtime operations",
	}
	cmd.AddCommand(
		newCacheInvalidateCmd(),
		newCacheWarmupCmd(),
		newCacheReloadCmd(),
		newCacheStatsCmd(),
	)
	return cmd
}

func newCacheInvalidateCmd() *cobra.Command {
	var tenant string
	c := &cobra.Command{
		Use:   "invalidate",
		Short: "Invalidate cache for a tenant",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" {
				return jsonErr("missing_tenant", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Cache.LicenseStore.InvalidateTenant(ctx, tenant); err != nil {
				return jsonErr("invalidate_license_failed", err)
			}
			appContainer.Deps.Services.Cache.TenantStore.InvalidateByTenantID(ctx, tenant)
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	return c
}

func newCacheWarmupCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "warmup",
		Short: "Warm up hot cache entries (bounded)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				limit = appContainer.Deps.Services.Sys.CacheConf.WarmUpLicenseLimit
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Cache.LicenseStore.WarmUp(ctx, appContainer.Deps.Services.Repo.Licenses, limit); err != nil {
				return jsonErr("warmup_license_failed", err)
			}
			// bounded tenant warmup using FindAll but capped by WarmUpTenantLimit
			max := appContainer.Deps.Services.Sys.CacheConf.WarmUpTenantLimit
			if max > 0 {
				tenants, err := appContainer.Deps.Services.Repo.Tenants.FindAll(ctx)
				if err == nil {
					if max > len(tenants) {
						max = len(tenants)
					}
					for i := 0; i < max; i++ {
						t := tenants[i]
						appContainer.Deps.Services.Cache.TenantStore.Set(ctx, t.ID, t.APIKey, t)
						if t.OldAPIKey != "" {
							appContainer.Deps.Services.Cache.TenantStore.Set(ctx, t.ID, t.OldAPIKey, t)
						}
					}
				}
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().IntVar(&limit, "limit", 0, "Max licenses to warm")
	return c
}

func newCacheReloadCmd() *cobra.Command {
	var tenant string
	var limit int
	c := &cobra.Command{
		Use:   "reload",
		Short: "Invalidate and warmup (bounded); optional tenant scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if tenant != "" {
				if err := appContainer.Deps.Services.Cache.LicenseStore.InvalidateTenant(ctx, tenant); err != nil {
					return jsonErr("invalidate_license_failed", err)
				}
				appContainer.Deps.Services.Cache.TenantStore.InvalidateByTenantID(ctx, tenant)
				// warmup won't scope to tenant for licenses (no repo method for that yet),
				// but tenant cache was invalidated and will repopulate on control-plane writes.
				jsonOut(map[string]any{"status": "ok"})
				return nil
			}
			// global bounded warmup (no full scans)
			if limit <= 0 {
				limit = appContainer.Deps.Services.Sys.CacheConf.WarmUpLicenseLimit
			}
			if err := appContainer.Deps.Services.Cache.LicenseStore.WarmUp(ctx, appContainer.Deps.Services.Repo.Licenses, limit); err != nil {
				return jsonErr("warmup_license_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID (optional)")
	c.Flags().IntVar(&limit, "limit", 0, "Max licenses to warm (global)")
	return c
}

func newCacheStatsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "stats",
		Short: "Show cache configuration and basic stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			conf := appContainer.Deps.Services.Sys.CacheConf

			out := map[string]any{
				"l2": map[string]any{
					"enabled": appContainer.Deps.Services.Cache.L2 != nil,
				},
				"ttl": map[string]any{
					"license_l1":        conf.LicenseTTLL1.String(),
					"license_l2":        conf.LicenseTTLL2.String(),
					"license_active":    conf.LicenseTTLActive.String(),
					"license_negative":  conf.LicenseTTLNegative.String(),
					"tenant":            conf.TenantTTL.String(),
					"tenant_negative":   conf.TenantTTLNegative.String(),
				},
			}
			jsonOut(out)
			return nil
		},
	}
	return c
}

