package permissions

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

type PermissionRegistry struct {
	db *db.DB
}

func NewRegistry(db *db.DB) *PermissionRegistry {
	return &PermissionRegistry{
		db: db,
	}
}

func (r *PermissionRegistry) GetPermission(ctx context.Context, userID, repositoryID int64) (gen.Permission, error) {
	return r.db.GetPermission(ctx, gen.GetPermissionParams{
		UserID:       userID,
		RepositoryID: repositoryID,
	})
}

func (r *PermissionRegistry) UpsertPermission(ctx context.Context, userID, repositoryID int64, role string) (gen.Permission, error) {
	return r.db.UpsertPermission(ctx, gen.UpsertPermissionParams{
		UserID:       userID,
		RepositoryID: repositoryID,
		UserRole:     role,
	})
}

func (r *PermissionRegistry) DeletePermission(ctx context.Context, userID, repositoryID int64) error {
	return r.db.DeletePermission(ctx, gen.DeletePermissionParams{
		UserID:       userID,
		RepositoryID: repositoryID,
	})
}
