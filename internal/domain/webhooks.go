package domain

import "time"

type Webhook struct {
	ID           int64
	RepositoryID int64
	URL          string
	Secret       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RepositoryEvent struct {
	Event string

	RepositoryID       int64
	RepositoryName     string
	RepositoryFullName string // namespace/name

	PusherID       int64
	PusherUsername string

	Ref    string
	OldSHA string
	NewSHA string

	Timestamp time.Time
}
