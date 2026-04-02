package handlers

import (
	"context"
	"github.com/devravik/go-license-api/internal/audit"
	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/infrastructure/cache"
	"github.com/devravik/go-license-api/internal/security"
	"github.com/devravik/go-license-api/internal/setup"
	"github.com/devravik/go-license-api/internal/worker"
)

type Handler struct {
	Cfg               *setup.Config
	// Readiness and diagnostics providers are injected by the server.
	IsReady           func() bool
	QueueDepth        func() int
	ValidationService app.ValidationService
	ActivationService app.ActivationService
	AdminService      app.AdminService
	Pool              *worker.Pool
	IdempCache        *IdempotencyCache
	LicenseStore      *cache.LicenseStore
	SignerRegistry    *crypto.SignerRegistry
	AuditQuery        *audit.QueryService
	WebhookEncKey     []byte
	WebhookRepo       WebhookWriter
}

type WebhookWriter interface {
	Create(ctx context.Context, id, tenantID, url string, events []string, secretEnc []byte) error
}

func NewHandler(
	cfg *setup.Config,
	valSvc app.ValidationService,
	activationSvc app.ActivationService,
	adminSvc app.AdminService,
	pool *worker.Pool,
	idempCache *IdempotencyCache,
	licenseStore *cache.LicenseStore,
	signerRegistry *crypto.SignerRegistry,
	auditQuery *audit.QueryService,
	webhookEncKey []byte,
	webhookRepo WebhookWriter,
) *Handler {
	return &Handler{
		Cfg:               cfg,
		IsReady:           func() bool { return true },
		QueueDepth:        func() int { if pool != nil { return pool.QueueDepth() }; return 0 },
		ValidationService: valSvc,
		ActivationService: activationSvc,
		AdminService:      adminSvc,
		Pool:              pool,
		IdempCache:        idempCache,
		LicenseStore:      licenseStore,
		SignerRegistry:    signerRegistry,
		AuditQuery:        auditQuery,
		WebhookEncKey:     webhookEncKey,
		WebhookRepo:       webhookRepo,
	}
}
