package webhooks

import (
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

// the all-zero object id git uses for a missing ref side (create's before,
// delete's after)
const zeroSHA = "0000000000000000000000000000000000000000"

type WebhookPayload struct {
	Event   string `json:"event"`
	Ref     string `json:"ref"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Created bool   `json:"created"`
	Deleted bool   `json:"deleted"`

	Repository WebhookRepository `json:"repository"`
	Pusher     WebhookPusher     `json:"pusher"`

	Timestamp time.Time `json:"timestamp"`
}

type WebhookRepository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

type WebhookPusher struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func newWebhookPayload(e domain.RepositoryEvent) WebhookPayload {
	return WebhookPayload{
		Event:   e.Event,
		Ref:     e.Ref,
		Before:  e.OldSHA,
		After:   e.NewSHA,
		Created: e.OldSHA == zeroSHA,
		Deleted: e.NewSHA == zeroSHA,
		Repository: WebhookRepository{
			ID:       e.RepositoryID,
			Name:     e.RepositoryName,
			FullName: e.RepositoryFullName,
		},
		Pusher: WebhookPusher{
			ID:       e.PusherID,
			Username: e.PusherUsername,
		},
		Timestamp: e.Timestamp,
	}
}
