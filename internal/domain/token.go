package domain

import "time"

type Token struct {
	ID         int64
	Title      string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}
