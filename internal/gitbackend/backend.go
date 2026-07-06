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
	ListTree(ctx context.Context, storagePath, rev, treePath string) (TreeListing, error)
	ResolveCommit(ctx context.Context, storagePath, rev string) (string, error)
	ArchiveTar(ctx context.Context, storagePath, rev string, out io.Writer) (string, error)
	StatBlob(ctx context.Context, storagePath, rev, treePath string) (BlobInfo, error)
	ReadBlob(ctx context.Context, storagePath, blobSHA string, out io.Writer) error
	WriteBlob(ctx context.Context, storagePath string, r io.Reader) (string, int64, error)
	ApplyCommit(ctx context.Context, storagePath string, spec CommitSpec, ops []CommitOp, clean CleanFunc) (RefChange, error)
}
