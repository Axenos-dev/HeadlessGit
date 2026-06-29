package webhooks

type WebhookPayload struct {
	Event        string `json:"event"`
	RepositoryID int64  `json:"repository_id"`
}
