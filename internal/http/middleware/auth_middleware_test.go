package middleware_test

import (
	"net/http/httptest"
	"testing"

	"github.com/devravik/go-license-api/internal/http/middleware"
	"github.com/gofiber/fiber/v3"
)

func TestAdminKeyGuard_RejectsMissingKey(t *testing.T) {
	app := fiber.New()
	app.Get("/admin", middleware.AdminKeyGuard("secret"), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestAdminKeyGuard_AcceptsValidKey(t *testing.T) {
	app := fiber.New()
	app.Get("/admin", middleware.AdminKeyGuard("secret"), func(c fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("X-Admin-Key", "secret")
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}
