package webhooks

import (
	"errors"
	"net/url"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type CreateWebhookRequest struct {
	URL string `json:"url"`
}

func (r CreateWebhookRequest) Validate() error {
	if r.URL == "" {
		return errors.New("url is required")
	}
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("url must be a valid http(s) URL")
	}
	return nil
}

type WebhookResponse struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret"`
	CreatedAt time.Time `json:"createdAt"`
}

func newWebhookResponse(w domain.Webhook) WebhookResponse {
	return WebhookResponse{
		ID:        w.ID,
		URL:       w.URL,
		Secret:    w.Secret,
		CreatedAt: w.CreatedAt,
	}
}

type WebhookListItem struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

func newWebhookListItems(webhooks []domain.Webhook) []WebhookListItem {
	out := make([]WebhookListItem, len(webhooks))
	for i, w := range webhooks {
		out[i] = WebhookListItem{ID: w.ID, URL: w.URL, CreatedAt: w.CreatedAt}
	}
	return out
}
