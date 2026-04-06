package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Tenant management commands",
	}

	cmd.AddCommand(
		newTenantBootstrapCmd(),
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

func newTenantBootstrapCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Interactively create first tenant and print raw API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)
			ask := func(label, def string) (string, error) {
				if def != "" {
					fmt.Printf("%s [%s]: ", label, def)
				} else {
					fmt.Printf("%s: ", label)
				}
				v, err := reader.ReadString('\n')
				if err != nil {
					return "", err
				}
				v = strings.TrimSpace(v)
				if v == "" {
					return def, nil
				}
				return v, nil
			}
			parseInt := func(v string, def int) (int, error) {
				v = strings.TrimSpace(v)
				if v == "" {
					return def, nil
				}
				n, err := strconv.Atoi(v)
				if err != nil {
					return 0, err
				}
				return n, nil
			}

			name, err := ask("Tenant name", "")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			slug, err := ask("Tenant slug", "")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			email, err := ask("Contact email", "")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			company, err := ask("Company", "")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			plan, err := ask("Plan", "starter")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			rpsRaw, err := ask("Rate limit RPS", "100")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			rps, err := parseInt(rpsRaw, 100)
			if err != nil || rps <= 0 {
				return jsonErr("invalid_rps", fmt.Errorf("rps must be a positive integer"))
			}
			burstRaw, err := ask("Rate limit burst", "200")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			burst, err := parseInt(burstRaw, 200)
			if err != nil || burst <= 0 {
				return jsonErr("invalid_burst", fmt.Errorf("burst must be a positive integer"))
			}
			maxLicRaw, err := ask("Max licenses", "1000")
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}
			maxLicenses, err := parseInt(maxLicRaw, 1000)
			if err != nil || maxLicenses < 0 {
				return jsonErr("invalid_max_licenses", fmt.Errorf("max_licenses must be >= 0"))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			tenant, apiKey, err := appContainer.Deps.Services.Admin.Admin.CreateTenant(ctx, rps, burst)
			if err != nil {
				return jsonErr("tenant_bootstrap_failed", err)
			}

			// Best-effort profile update for explicit onboarding fields.
			if u, ok := appContainer.Deps.Services.Admin.Admin.(interface {
				UpdateTenantProfile(ctx context.Context, tenantID string, name, slug, email, company, plan string, maxLicenses int, metadata map[string]any) error
			}); ok {
				if err := u.UpdateTenantProfile(ctx, tenant.ID, name, slug, email, company, plan, maxLicenses, nil); err != nil {
					return jsonErr("tenant_profile_update_failed", err)
				}
				updated, gerr := appContainer.Deps.Services.Repo.Tenants.FindByID(ctx, tenant.ID)
				if gerr == nil && updated != nil {
					tenant = updated
				}
			}

			// Human-friendly bootstrap output; keep api key prominent and explicit.
			fmt.Println()
			fmt.Println("==========================================")
			fmt.Println(" Tenant bootstrap completed successfully")
			fmt.Println("==========================================")
			fmt.Println("Tenant:")
			fmt.Printf("  %-13s %s\n", "ID:", tenant.ID)
			fmt.Printf("  %-13s %s\n", "Name:", tenant.Name)
			fmt.Printf("  %-13s %s\n", "Slug:", tenant.Slug)
			fmt.Printf("  %-13s %s\n", "Email:", tenant.Email)
			fmt.Printf("  %-13s %s\n", "Company:", tenant.Company)
			fmt.Printf("  %-13s %s\n", "Plan:", tenant.Plan)
			fmt.Printf("  %-13s %s\n", "Status:", tenant.Status)
			fmt.Printf("  %-13s %d\n", "RPS:", tenant.RPS)
			fmt.Printf("  %-13s %d\n", "Burst:", tenant.Burst)
			fmt.Printf("  %-13s %d\n", "Max licenses:", tenant.MaxLicenses)
			fmt.Println()
			fmt.Println("Raw API key (shown once):")
			fmt.Printf("  %s\n", apiKey)
			fmt.Println()
			fmt.Println("IMPORTANT:")
			fmt.Println("  Save this raw API key now. Only its hash is stored in the database.")
			fmt.Println("  Use it in requests as header: X-API-Key: <raw_api_key>")
			return nil
		},
	}
	return c
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

