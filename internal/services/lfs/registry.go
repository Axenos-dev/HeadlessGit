package lfs

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

type LFSRegistry struct {
	db *db.DB
}

func NewRegistry(db *db.DB) *LFSRegistry {
	return &LFSRegistry{
		db: db,
	}
}

func (r *LFSRegistry) CreateLFSObject(ctx context.Context, userID, repositoryID int64, objectID string, sizeBytes int64, storageKey string) (gen.LfsObject, error) {
	return r.db.CreateLFSObject(ctx, gen.CreateLFSObjectParams{
		UserID:       userID,
		RepositoryID: repositoryID,
		ObjectID:     objectID,
		SizeBytes:    sizeBytes,
		StorageKey:   storageKey,
	})
}

func (r *LFSRegistry) CreateVerifiedLFSObject(ctx context.Context, userID, repositoryID int64, objectID string, sizeBytes int64, storageKey string) (gen.LfsObject, error) {
	return r.db.CreateVerifiedLFSObject(ctx, gen.CreateVerifiedLFSObjectParams{
		UserID:       userID,
		RepositoryID: repositoryID,
		ObjectID:     objectID,
		SizeBytes:    sizeBytes,
		StorageKey:   storageKey,
	})
}

func (r *LFSRegistry) GetLFSObject(ctx context.Context, repositoryID int64, objectID string) (gen.LfsObject, error) {
	return r.db.GetLFSObject(ctx, gen.GetLFSObjectParams{
		ObjectID:     objectID,
		RepositoryID: repositoryID,
	})
}

func (r *LFSRegistry) DeleteLFSObject(ctx context.Context, repositoryID int64, objectID string) error {
	return r.db.DeleteLFSObject(ctx, gen.DeleteLFSObjectParams{
		ObjectID:     objectID,
		RepositoryID: repositoryID,
	})
}

func (r *LFSRegistry) SetLFSObjectVerified(ctx context.Context, repositoryID int64, objectID string, verified bool) (gen.LfsObject, error) {
	return r.db.SetLFSObjectVerified(ctx, gen.SetLFSObjectVerifiedParams{
		Verified:     verified,
		ObjectID:     objectID,
		RepositoryID: repositoryID,
	})
}

func (r *LFSRegistry) VerifyLFSObjectAtKey(ctx context.Context, userID, repositoryID int64, objectID string, sizeBytes int64, storageKey string) (gen.LfsObject, error) {
	return r.db.VerifyLFSObjectAtKey(ctx, gen.VerifyLFSObjectAtKeyParams{
		UserID:       userID,
		RepositoryID: repositoryID,
		ObjectID:     objectID,
		SizeBytes:    sizeBytes,
		StorageKey:   storageKey,
	})
}
