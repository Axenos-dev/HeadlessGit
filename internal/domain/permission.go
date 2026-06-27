package domain

import "time"

// Permission is an explicit collaborator grant on a repository.
type Permission struct {
	UserID    int64
	Role      Role
	CreatedAt time.Time
	UpdatedAt *time.Time
}
