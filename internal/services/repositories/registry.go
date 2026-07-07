package repositories

import (
	"context"
	"database/sql"

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

func (r *RepositoryRegistry) UpdateRepositoryVisibility(ctx context.Context, repositoryID int64, visibility string) (gen.Repository, error) {
	return r.db.UpdateRepositoryVisibility(ctx, gen.UpdateRepositoryVisibilityParams{
		Visibility: visibility,
		ID:         repositoryID,
	})
}

func (r *RepositoryRegistry) ListUserRepositories(ctx context.Context, ownerID int64) ([]gen.Repository, error) {
	return r.db.ListUserRepositories(ctx, ownerID)
}

func (r *RepositoryRegistry) ListRepositories(ctx context.Context) ([]gen.Repository, error) {
	return r.db.ListRepositories(ctx)
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

func (r *RepositoryRegistry) CreateRepositoryPathPolicy(ctx context.Context, repositoryID int64, kind, pattern string, reason *string) (gen.PathPolicy, error) {
	var reasonNS sql.NullString
	if reason != nil {
		reasonNS = sql.NullString{String: *reason, Valid: true}
	}
	return r.db.CreatePathPolicy(ctx, gen.CreatePathPolicyParams{
		RepositoryID: repositoryID,
		Kind:         kind,
		Pattern:      pattern,
		Reason:       reasonNS,
	})
}

func (r *RepositoryRegistry) ListRepositoryPathPolicies(ctx context.Context, repositoryID int64) ([]gen.PathPolicy, error) {
	return r.db.ListRepositoryPathPolicies(ctx, repositoryID)
}

func (r *RepositoryRegistry) DeleteRepositoryPathPolicy(ctx context.Context, repositoryID, pathPolicyID int64) error {
	return r.db.DeletePathPolicy(ctx, gen.DeletePathPolicyParams{
		ID:           pathPolicyID,
		RepositoryID: repositoryID,
	})
}
