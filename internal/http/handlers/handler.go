package handlers

import (
	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/worker"
)

type Handler struct {
	Cfg               *configs.Config
	ValidationService app.ValidationService
	ActivationService app.ActivationService
	AdminService      app.AdminService
	Pool              *worker.Pool
	IdempCache        *IdempotencyCache
}

func NewHandler(
	cfg *configs.Config,
	valSvc app.ValidationService,
	activationSvc app.ActivationService,
	adminSvc app.AdminService,
	pool *worker.Pool,
	idempCache *IdempotencyCache,
) *Handler {
	return &Handler{
		Cfg:               cfg,
		ValidationService: valSvc,
		ActivationService: activationSvc,
		AdminService:      adminSvc,
		Pool:              pool,
		IdempCache:        idempCache,
	}
}
