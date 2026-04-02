package main

import (
	"context"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newProductCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "product",
		Short: "Product management commands",
	}
	cmd.AddCommand(
		newProductCreateCmd(),
		newProductUpdateCmd(),
		newProductDeleteCmd(),
		newProductGetCmd(),
		newProductListCmd(),
		newProductActivateCmd(true),
		newProductActivateCmd(false),
	)
	return cmd
}

func newProductCreateCmd() *cobra.Command {
	var tenant, code, name, version string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a product (upsert)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || code == "" || name == "" {
				return jsonErr("missing_required_fields", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			var ver *string
			if version != "" {
				v := version
				ver = &v
			}
			p := &domain.Product{
				ID:       uuid.New().String(),
				TenantID: tenant,
				Code:     code,
				Name:     name,
				Version:  ver,
				IsActive: true,
			}
			if err := appContainer.Deps.Services.Repo.Products.Upsert(ctx, p); err != nil {
				return jsonErr("product_upsert_failed", err)
			}
			jsonOut(map[string]any{"status": "ok", "id": p.ID})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&code, "id", "", "Product code (stable identifier)")
	c.Flags().StringVar(&name, "name", "", "Product name")
	c.Flags().StringVar(&version, "version", "", "Product version")
	return c
}

func newProductUpdateCmd() *cobra.Command {
	var tenant, code, name, version string
	c := &cobra.Command{
		Use:   "update",
		Short: "Update a product (upsert)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || code == "" {
				return jsonErr("missing_required_fields", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			existing, _ := appContainer.Deps.Services.Repo.Products.FindByCode(ctx, tenant, code)
			id := ""
			if existing != nil {
				id = existing.ID
			} else {
				id = uuid.New().String()
			}
			var ver *string
			if version != "" {
				v := version
				ver = &v
			}
			p := &domain.Product{
				ID:       id,
				TenantID: tenant,
				Code:     code,
				Name:     name,
				Version:  ver,
				IsActive: true,
			}
			if err := appContainer.Deps.Services.Repo.Products.Upsert(ctx, p); err != nil {
				return jsonErr("product_upsert_failed", err)
			}
			jsonOut(map[string]any{"status": "ok", "id": id})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&code, "id", "", "Product code (stable identifier)")
	c.Flags().StringVar(&name, "name", "", "Product name")
	c.Flags().StringVar(&version, "version", "", "Product version")
	return c
}

func newProductDeleteCmd() *cobra.Command {
	var tenant, productID string
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete a product by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || productID == "" {
				return jsonErr("missing_required_fields", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Repo.Products.Delete(ctx, tenant, productID); err != nil {
				return jsonErr("product_delete_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&productID, "id", "", "Product ID")
	return c
}

func newProductGetCmd() *cobra.Command {
	var tenant, productID, code string
	c := &cobra.Command{
		Use:   "get",
		Short: "Get a product by ID or code",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" {
				return jsonErr("missing_tenant", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if productID != "" {
				p, err := appContainer.Deps.Services.Repo.Products.FindByID(ctx, tenant, productID)
				if err != nil {
					return jsonErr("product_get_failed", err)
				}
				jsonOut(p)
				return nil
			}
			if code != "" {
				p, err := appContainer.Deps.Services.Repo.Products.FindByCode(ctx, tenant, code)
				if err != nil {
					return jsonErr("product_get_failed", err)
				}
				jsonOut(p)
				return nil
			}
			return jsonErr("missing_id_or_code", nil)
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&productID, "id", "", "Product ID")
	c.Flags().StringVar(&code, "code", "", "Product code")
	return c
}

func newProductListCmd() *cobra.Command {
	var tenant string
	c := &cobra.Command{
		Use:   "list",
		Short: "List products for a tenant",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" {
				return jsonErr("missing_tenant", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			ps, err := appContainer.Deps.Services.Repo.Products.ListByTenant(ctx, tenant)
			if err != nil {
				return jsonErr("product_list_failed", err)
			}
			jsonOut(map[string]any{"products": ps})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	return c
}

func newProductActivateCmd(active bool) *cobra.Command {
	var tenant, id string
	name := "activate"
	desc := "Mark product active"
	if !active {
		name = "deactivate"
		desc = "Mark product inactive"
	}
	c := &cobra.Command{
		Use:   name,
		Short: desc,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tenant == "" || id == "" {
				return jsonErr("missing_required_fields", nil)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), appContainer.Deps.Config.Timeout)
			defer cancel()
			if err := appContainer.Deps.Services.Repo.Products.SetActive(ctx, tenant, id, active); err != nil {
				return jsonErr("product_set_active_failed", err)
			}
			jsonOut(map[string]any{"status": "ok"})
			return nil
		},
	}
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant ID")
	c.Flags().StringVar(&id, "id", "", "Product ID")
	return c
}

