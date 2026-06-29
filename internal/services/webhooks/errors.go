package webhooks

import "errors"

var (
	ErrEventsChannelFull = errors.New("events channel buffer is full")
)
