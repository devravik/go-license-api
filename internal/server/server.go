package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/http"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	jsoniter "github.com/json-iterator/go"
)

func New() (*fiber.App, *configs.Config) {
	cfg := configs.Load()
	logCfg := configs.LoadLoggingConfig()

	fiberCfg := fiber.Config{
		AppName:      cfg.AppName,
		ServerHeader: "",

		// JSON handling
		JSONEncoder: json.Marshal,
		JSONDecoder: json.Unmarshal,

		// Server-level timeouts (Network/TCP Layer)
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,

		// Body limits
		BodyLimit: 1024 * 1024,

		// Performance tuning
		DisableKeepalive:  false,
		ReduceMemoryUsage: true,

		// Error handling
		ErrorHandler: func(c fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError

			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			return c.Status(code).JSON(fiber.Map{
				"valid": false,
				"error": err.Error(),
			})
		},
	}

	if strings.EqualFold(cfg.JSONEngine, "jsoniter") {
		var json = jsoniter.ConfigFastest
		fiberCfg.JSONEncoder = json.Marshal
		fiberCfg.JSONDecoder = json.Unmarshal
	}

	appInstance := fiber.New(fiberCfg)

	// Middleware
	appInstance.Use(requestid.New())
	logCfg.Setup(appInstance)
	appInstance.Use(recover.New())

	// Initialize services
	valSvc := app.NewValidationService()

	// Setup routes with injected config and services
	http.SetupRoutes(appInstance, cfg, valSvc)

	return appInstance, cfg
}
