package webhooks

import (
	"context"
	"errors"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	webhookservice "github.com/Axenos-dev/HeadlessGit/internal/services/webhooks"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type WebhookManager interface {
	RegisterWebhook(ctx context.Context, repoID int64, url string) (domain.Webhook, error)
	DeleteWebhook(ctx context.Context, webhookID, repositoryID int64) error
	ListWebhooks(ctx context.Context, repositoryID int64) ([]domain.Webhook, error)
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
		r.Get("/", response.Handler(h.logger, h.listWebhooks))
		r.Delete("/{webhookID}", response.Handler(h.logger, h.deleteWebhook))
	})
}

// maps service errors for every route in this group, so one error always gets one response
func (h *handlers) webhooksError(err error, logMessage string) *response.APIError {
	switch {
	case errors.Is(err, webhookservice.ErrWebhookExists):
		return response.NewError(http.StatusConflict, response.CodeWebhookExists, "webhook already exists")
	default:
		h.logger.Error(logMessage, zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, logMessage)
	}
}
