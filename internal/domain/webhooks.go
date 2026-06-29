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
	RepositoryID int64
	PusherID     int64
	Event        string

	Ref    string
	OldSHA string
	NewSHA string
}
