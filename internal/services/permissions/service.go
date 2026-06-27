package permissions

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type Registry interface {
	GetPermission(ctx context.Context, userID, repositoryID int64) (gen.Permission, error)
	UpsertPermission(ctx context.Context, userID, repositoryID int64, role string) (gen.Permission, error)
	DeletePermission(ctx context.Context, userID, repositoryID int64) error
	ListRepositoryPermissions(ctx context.Context, repositoryID int64) ([]gen.Permission, error)
}

type Service struct {
	registry Registry
}

func NewService(registry Registry) *Service {
	return &Service{
		registry: registry,
	}
}

func (s *Service) Authorize(ctx context.Context, account *domain.Account, repo domain.Repository, required domain.Role) error {
	var effective domain.Role

	// public repos grant read to everyone, including anonymous
	if repo.Visibility == domain.RepoVisibilityPublic {
		effective = maxRole(effective, domain.RoleRead)
	}

	if account != nil {
		switch {
		case account.IsAdmin:
			// global operators have implicit admin on every repo
			effective = maxRole(effective, domain.RoleAdmin)
		case account.UserID == repo.OwnerID:
			// the owner always has admin perms
			effective = maxRole(effective, domain.RoleAdmin)
		default:
			// otherwise, effective role is the explicit grant
			role, err := s.grantedRole(ctx, account.UserID, repo.ID)
			if err != nil {
				return err
			}
			effective = maxRole(effective, role)
		}
	}

	if effective.AtLeast(required) {
		return nil
	}
	return ErrAccessDenied
}

func (s *Service) Grant(ctx context.Context, userID, repositoryID int64, role domain.Role) error {
	if role.Level() == 0 {
		return ErrInvalidRole
	}
	_, err := s.registry.UpsertPermission(ctx, userID, repositoryID, string(role))
	return err
}

func (s *Service) Revoke(ctx context.Context, userID, repositoryID int64) error {
	return s.registry.DeletePermission(ctx, userID, repositoryID)
}

func (s *Service) List(ctx context.Context, repositoryID int64) ([]domain.Permission, error) {
	perms, err := s.registry.ListRepositoryPermissions(ctx, repositoryID)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Permission, len(perms))
	for i, p := range perms {
		out[i] = toDomain(p)
	}
	return out, nil
}

func toDomain(p gen.Permission) domain.Permission {
	perm := domain.Permission{
		UserID:    p.UserID,
		Role:      domain.Role(p.UserRole),
		CreatedAt: time.UnixMilli(p.CreatedAtUnixMs).UTC(),
	}
	if p.UpdatedAtUnixMs.Valid {
		t := time.UnixMilli(p.UpdatedAtUnixMs.Int64).UTC()
		perm.UpdatedAt = &t
	}
	return perm
}

// returns explicit permission role,
// if no rows -> empty role "" -> level=0
func (s *Service) grantedRole(ctx context.Context, userID, repositoryID int64) (domain.Role, error) {
	perm, err := s.registry.GetPermission(ctx, userID, repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Role(""), nil
	}
	if err != nil {
		return domain.Role(""), err
	}
	return domain.Role(perm.UserRole), nil
}

// returns role with maximum level between a and b
func maxRole(a, b domain.Role) domain.Role {
	if a.Level() >= b.Level() {
		return a
	}
	return b
}
