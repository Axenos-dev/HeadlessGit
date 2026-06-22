package domain

import "time"

type RepoVisibility string

const (
	RepoVisibilityPublic  RepoVisibility = "public"
	RepoVisibilityPrivate RepoVisibility = "private"
)

type RepositoryInfo struct {
	RepositoryName string
	Visibility     RepoVisibility
}

type Repository struct {
	ID             int64
	OwnerID        int64
	RepositoryName string
	StoragePath    string
	Visibility     RepoVisibility
	CreatedAt      time.Time
	UpdatedAt      *time.Time
}
