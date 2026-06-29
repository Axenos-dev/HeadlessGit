package webhooks

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type WebhookManager interface {
	RegisterWebhook(ctx context.Context, repoID int64, url string) (domain.Webhook, error)
	DeleteWebhook(ctx context.Context, webhookID, repositoryID int64) error
}

type handlers struct {
	logger   *zap.Logger
	webhooks WebhookManager
}

func NewHandlers(logger *zap.Logger, webhooks WebhookManager) *handlers {
	return &handlers{
		logger:   logger,
		webhooks: webhooks,
	}
}

func (h *handlers) RegisterRoutes(parent chi.Router) {
	parent.Route("/repositories/{repositoryID}/webhooks", func(r chi.Router) {
		r.Post("/", response.Handler(h.logger, h.createWebhook))
		r.Delete("/{webhookID}", response.Handler(h.logger, h.deleteWebhook))
	})
}
