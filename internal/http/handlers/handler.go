package handlers

import (
	"github.com/devravik/go-license-api/configs"
	"github.com/devravik/go-license-api/internal/app"
)

type Handler struct {
	Cfg               *configs.Config
	ValidationService app.ValidationService
}

func NewHandler(cfg *configs.Config, valSvc app.ValidationService) *Handler {
	return &Handler{
		Cfg:               cfg,
		ValidationService: valSvc,
	}
}
