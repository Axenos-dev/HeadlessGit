package storage

import (
	"context"
	"io"
	"time"
)

// storage is a polymorphic thing
// it can either disk (store objects locally)
// or S3-compatible bucket
type Storage interface {
	Stat(ctx context.Context, key string) (exists bool, size int64, err error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, size int64, r io.Reader) error
	Delete(ctx context.Context, key string) error
}

// presigner is more an optional capability
// something like local disk, does not implement this
type Presigner interface {
	PresignPut(ctx context.Context, key string, size int64, ttl time.Duration) (url string, header map[string]string, err error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error)
}
