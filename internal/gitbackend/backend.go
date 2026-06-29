package gitbackend

import (
	"context"
	"io"
)

// git backend is a polymorphic thing,
// it can be either local (we store bare repos on a disk),
// or the repos itself we store on storage nodes (coming soon)
type Backend interface {
	AdvertiseRefs(ctx context.Context, storagePath string, svc Service, stdout io.Writer) error
	UploadPack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) error
	ReceivePack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) ([]RefChange, error)
}
