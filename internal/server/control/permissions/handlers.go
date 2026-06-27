package permissions

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type PermissionManager interface {
	Grant(ctx context.Context, userID, repositoryID int64, role domain.Role) error
	Revoke(ctx context.Context, userID, repositoryID int64) error
	List(ctx context.Context, repositoryID int64) ([]domain.Permission, error)
}

type handlers struct {
	logger *zap.Logger
	perms  PermissionManager
}

func NewHandlers(logger *zap.Logger, perms PermissionManager) *handlers {
	return &handlers{
		logger: logger,
		perms:  perms,
	}
}

func (h *handlers) RegisterRoutes(parent chi.Router) {
	parent.Route("/repositories/{repositoryID}/permissions", func(r chi.Router) {
		r.Get("/", response.Handler(h.logger, h.listPermissions))
		r.Put("/", response.Handler(h.logger, h.grantPermission))
		r.Delete("/{userID}", response.Handler(h.logger, h.revokePermission))
	})
}
