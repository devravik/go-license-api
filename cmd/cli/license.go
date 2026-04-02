package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/spf13/cobra"
)

func newLicenseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "license",
		Short: "License management commands",
	}
	cmd.AddCommand(
		newLicenseCreateCmd(),
		newLicenseUpdateCmd(),
		newLicenseRevokeCmd(),
		newLicenseGetCmd(),
		newLicenseListCmd(),
	)
	return cmd
}

func newLicenseCreateCmd() *cobra.Command {
	var tenant, key, product, expires, meta string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a license (write-through cache)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || key == "" {
				return jsonErr("missing_tenant_or_key", nil)
			}
			var expPtr *time.Time
			if expires != "" {
				t, err := time.Parse("2006-01-02", expires)
				if err != nil {
					return jsonErr("invalid_expires", err)
				}
				expPtr = &t
			}
			var metaMap map[string]any
			if meta != "" {
				if err := json.Unmarshal([]byte(meta), &metaMap); err != nil {
					return jsonErr("invalid_meta_json", err)
				}
			}
			lic := &domain.License{
				TenantID: tenant,
				Key:      key,
				Product:  product,
				Meta:     metaMap,
			}
			if expPtr != nil {
				lic.ExpiresAt = expPtr
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Repo.Licenses.Create(ctx, lic); err != nil {
				return jsonErr("license_create_failed", err)
			}
			appContainer.Deps.Services.Cache.LicenseStore.Set(ctx, tenant, key, lic)
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&key, "key", "", "License key")
	c.Flags().StringVar(&product, "product", "", "Product code")
	c.Flags().StringVar(&expires, "expires", "", "Expiry date YYYY-MM-DD")
	c.Flags().StringVar(&meta, "meta", "{}", "JSON metadata")
	return c
}

func newLicenseUpdateCmd() *cobra.Command {
	var tenant, key, expires, meta string
	c := &cobra.Command{
		Use:   "update",
		Short: "Update license fields (write-through cache)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || key == "" {
				return jsonErr("missing_tenant_or_key", nil)
			}
			var expPtr *time.Time
			if expires != "" {
				t, err := time.Parse("2006-01-02", expires)
				if err != nil {
					return jsonErr("invalid_expires", err)
				}
				expPtr = &t
			}
			var metaMap map[string]any
			if meta != "" {
				if err := json.Unmarshal([]byte(meta), &metaMap); err != nil {
					return jsonErr("invalid_meta_json", err)
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			lic, err := appContainer.Deps.Services.Repo.Licenses.FindByKey(ctx, tenant, key)
			if err != nil {
				return jsonErr("license_not_found", err)
			}
			if expPtr != nil {
				lic.ExpiresAt = expPtr
			}
			if meta != "" {
				lic.Meta = metaMap
			}
			if err := appContainer.Deps.Services.Repo.Licenses.Update(ctx, lic); err != nil {
				return jsonErr("license_update_failed", err)
			}
			appContainer.Deps.Services.Cache.LicenseStore.Set(ctx, tenant, key, lic)
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&key, "key", "", "License key")
	c.Flags().StringVar(&expires, "expires", "", "Expiry date YYYY-MM-DD")
	c.Flags().StringVar(&meta, "meta", "", "JSON metadata")
	return c
}

func newLicenseRevokeCmd() *cobra.Command {
	var tenant, key string
	c := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a license (admin service handles cache)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || key == "" {
				return jsonErr("missing_tenant_or_key", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Admin.Admin.RevokeLicense(ctx, tenant, key); err != nil {
				return jsonErr("license_revoke_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&key, "key", "", "License key")
	return c
}

func newLicenseGetCmd() *cobra.Command {
	var tenant, key string
	c := &cobra.Command{
		Use:   "get",
		Short: "Get a license by key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || key == "" {
				return jsonErr("missing_tenant_or_key", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			lic, err := appContainer.Deps.Services.Repo.Licenses.FindByKey(ctx, tenant, key)
			if err != nil {
				return jsonErr("license_get_failed", err)
			}
			jsonOut(lic)
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&key, "key", "", "License key")
	return c
}

func newLicenseListCmd() *cobra.Command {
	var tenant string
	var limit, offset int
	c := &cobra.Command{
		Use:   "list",
		Short: "List licenses by tenant (bounded)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" {
				return jsonErr("missing_tenant", nil)
			}
			if limit <= 0 {
				limit = 100
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			list, err := appContainer.Deps.Services.Repo.Licenses.ListByTenant(ctx, tenant, limit, offset)
			if err != nil {
				return jsonErr("license_list_failed", err)
			}
			jsonOut(map[string]any{"licenses": list})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().IntVar(&limit, "limit", 100, "Limit")
	c.Flags().IntVar(&offset, "offset", 0, "Offset")
	return c
}

