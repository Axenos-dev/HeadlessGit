package repositories

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

type RepositoryRegistry struct {
	db *db.DB
}

func NewRegistry(db *db.DB) *RepositoryRegistry {
	return &RepositoryRegistry{
		db: db,
	}
}

func (r *RepositoryRegistry) GetRepository(ctx context.Context, repositoryID int64) (gen.Repository, error) {
	return r.db.GetRepository(ctx, repositoryID)
}

func (r *RepositoryRegistry) DeleteRepository(ctx context.Context, repositoryID int64) error {
	return r.db.DeleteRepository(ctx, repositoryID)
}

func (r *RepositoryRegistry) CreateRepository(ctx context.Context, ownerID int64, name, storagePath, visibility string) (gen.Repository, error) {
	return r.db.CreateRepository(ctx, gen.CreateRepositoryParams{
		OwnerID:        ownerID,
		RepositoryName: name,
		StoragePath:    storagePath,
		Visibility:     visibility,
	})
}

func (r *RepositoryRegistry) GetRepositoryByPath(ctx context.Context, namespace, name string) (gen.Repository, error) {
	return r.db.GetRepositoryByPath(ctx, gen.GetRepositoryByPathParams{
		Namespace: namespace,
		Name:      name,
	})
}
