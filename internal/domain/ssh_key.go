package domain

import "time"

type SSHKey struct {
	ID          int64
	Title       string
	Fingerprint string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
}
