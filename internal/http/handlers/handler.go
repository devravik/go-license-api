package handlers

import (
	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/worker"
)

type Handler struct {
	Cfg               *configs.Config
	ValidationService app.ValidationService
	AdminService      app.AdminService
	Pool              *worker.Pool
}

func NewHandler(cfg *configs.Config, valSvc app.ValidationService, adminSvc app.AdminService, pool *worker.Pool) *Handler {
	return &Handler{
		Cfg:               cfg,
		ValidationService: valSvc,
		AdminService:      adminSvc,
		Pool:              pool,
	}
}
