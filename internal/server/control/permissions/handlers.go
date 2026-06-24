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
	parent.Put("/repositories/{repositoryID}/permissions", response.Handler(h.logger, h.grantPermission))
}
