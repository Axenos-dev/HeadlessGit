package webhooks

import "errors"

var (
	ErrEventsChannelFull = errors.New("events channel buffer is full")
	ErrWebhookExists     = errors.New("webhook already exists")
)
