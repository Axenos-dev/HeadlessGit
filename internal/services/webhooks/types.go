package webhooks

type WebhookPayload struct {
	Event        string `json:"event"`
	RepositoryID int64  `json:"repository_id"`
	Ref          string `json:"ref"`
	OldSHA       string `json:"old_sha"`
	NewSHA       string `json:"new_sha"`
	PusherID     int64  `json:"pusher_id"`
}
