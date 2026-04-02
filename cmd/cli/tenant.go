package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Tenant management commands",
	}

	cmd.AddCommand(
		newTenantCreateCmd(),
		newTenantUpdateCmd(),
		newTenantDeleteCmd(),
		newTenantGetCmd(),
		newTenantListCmd(),
		newTenantRotateKeyCmd(),
		newTenantSuspendCmd(),
		newTenantReinstateCmd(),
		newTenantAllowlistCmd(),
	)

	return cmd
}

func newTenantCreateCmd() *cobra.Command {
	var rps, burst int
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a tenant with rate limits",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			tenant, apiKey, err := appContainer.Deps.Services.Admin.Admin.CreateTenant(ctx, rps, burst)
			if err != nil {
				return jsonErr("tenant_create_failed", err)
			}
			jsonOut(map[string]any{
				"tenant": tenant,
				"api_key": apiKey,
			})
			return nil
		},
	}
	c.Flags().IntVar(&rps, "rps", 100, "Requests per second")
	c.Flags().IntVar(&burst, "burst", 200, "Burst tokens")
	return c
}

func newTenantUpdateCmd() *cobra.Command {
	var id string
	var rps, burst int
	c := &cobra.Command{
		Use:   "update",
		Short: "Update tenant rate limits",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Admin.Admin.UpdateTenantLimits(ctx, id, rps, burst); err != nil {
				return jsonErr("tenant_update_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	c.Flags().IntVar(&rps, "rps", 100, "Requests per second")
	c.Flags().IntVar(&burst, "burst", 200, "Burst tokens")
	return c
}

func newTenantDeleteCmd() *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "delete",
		Short: "Soft-delete a tenant",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Admin.Admin.DeleteTenant(ctx, id); err != nil {
				return jsonErr("tenant_delete_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	return c
}

func newTenantGetCmd() *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "get",
		Short: "Get tenant by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			t, err := appContainer.Deps.Services.Repo.Tenants.FindByID(ctx, id)
			if err != nil {
				return jsonErr("tenant_get_failed", err)
			}
			jsonOut(t)
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	return c
}

func newTenantListCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List tenants",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			ts, err := appContainer.Deps.Services.Repo.Tenants.FindAll(ctx)
			if err != nil {
				return jsonErr("tenant_list_failed", err)
			}
			jsonOut(map[string]any{"tenants": ts})
			return nil
		},
	}
	return c
}

func newTenantRotateKeyCmd() *cobra.Command {
	var id string
	var grace time.Duration
	c := &cobra.Command{
		Use:   "rotate-key",
		Short: "Rotate tenant API key with grace period",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			newKey, until, err := appContainer.Deps.Services.Admin.Admin.RotateTenantAPIKey(ctx, id, grace)
			if err != nil {
				return jsonErr("tenant_rotate_key_failed", err)
			}
			jsonOut(map[string]any{
				"api_key":          newKey,
				"grace_period_end": until,
			})
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	c.Flags().DurationVar(&grace, "grace", 24*time.Hour, "Grace period duration")
	return c
}

func newTenantSuspendCmd() *cobra.Command {
	var id, reason string
	c := &cobra.Command{
		Use:   "suspend",
		Short: "Suspend tenant immediately",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Admin.Admin.SuspendTenant(ctx, id, reason); err != nil {
				return jsonErr("tenant_suspend_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	c.Flags().StringVar(&reason, "reason", "", "Reason for suspension")
	return c
}

func newTenantReinstateCmd() *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "reinstate",
		Short: "Reinstate a suspended tenant",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Admin.Admin.ReinstateTenant(ctx, id); err != nil {
				return jsonErr("tenant_reinstate_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	return c
}

func newTenantAllowlistCmd() *cobra.Command {
	var id string
	var cidrs []string
	c := &cobra.Command{
		Use:   "allowlist",
		Short: "Update tenant IP allowlist",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return jsonErr("missing_tenant_id", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Admin.Admin.UpdateTenantIPAllowlist(ctx, id, cidrs); err != nil {
				return jsonErr("tenant_allowlist_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant ID")
	c.Flags().StringArrayVar(&cidrs, "cidr", nil, "CIDR (multiple allowed)")
	return c
}

