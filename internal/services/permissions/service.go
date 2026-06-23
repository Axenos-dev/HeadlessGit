package permissions

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type Registry interface {
	GetPermission(ctx context.Context, userID, repositoryID int64) (gen.Permission, error)
	UpsertPermission(ctx context.Context, userID, repositoryID int64, role string) (gen.Permission, error)
	DeletePermission(ctx context.Context, userID, repositoryID int64) error
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
		if account.UserID == repo.OwnerID {
			// the owner always has admin perms
			effective = maxRole(effective, domain.RoleAdmin)
		} else {
			// otherwise, effective role is user role
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
