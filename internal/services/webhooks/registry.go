package webhooks

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

type WebhooksRegistry struct {
	db *db.DB
}

func NewRegistry(db *db.DB) *WebhooksRegistry {
	return &WebhooksRegistry{
		db: db,
	}
}

func (r *WebhooksRegistry) CreateWebhook(ctx context.Context, repoID int64, secret, url string) (gen.Webhook, error) {
	return r.db.CreateWebhook(ctx, gen.CreateWebhookParams{
		RepositoryID: repoID,
		Secret:       secret,
		Url:          url,
	})
}

func (r *WebhooksRegistry) ListWebhooksForRepository(ctx context.Context, repoID int64) ([]gen.Webhook, error) {
	return r.db.ListWebhooksForRepository(ctx, repoID)
}

func (r *WebhooksRegistry) DeleteWebhook(ctx context.Context, webhookID, repositoryID int64) error {
	return r.db.DeleteWebhook(ctx, gen.DeleteWebhookParams{
		ID:           webhookID,
		RepositoryID: repositoryID,
	})
}
