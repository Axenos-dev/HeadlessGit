package repositories

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type RepositoryManager interface {
	Create(ctx context.Context, ownerID int64, info domain.RepositoryInfo) (domain.Repository, error)
	Get(ctx context.Context, repositoryID int64) (domain.Repository, error)
	Delete(ctx context.Context, repositoryID int64) error
	SetVisibility(ctx context.Context, repositoryID int64, visibility domain.RepoVisibility) (domain.Repository, error)
	ListByOwner(ctx context.Context, ownerID int64) ([]domain.Repository, error)
}

type handlers struct {
	logger  *zap.Logger
	service RepositoryManager
}

func NewHandlers(logger *zap.Logger, service RepositoryManager) *handlers {
	return &handlers{
		logger:  logger,
		service: service,
	}
}

func (h *handlers) RegisterRoutes(parent chi.Router) {
	parent.Route("/repositories", func(r chi.Router) {
		r.Post("/", response.Handler(h.logger, h.createRepository))
		r.Get("/{repositoryID}", response.Handler(h.logger, h.getRepository))
		r.Put("/{repositoryID}/visibility", response.Handler(h.logger, h.setVisibility))
		r.Delete("/{repositoryID}", response.Handler(h.logger, h.deleteRepository))
	})

	// a user's repositories (by owner)
	parent.Get("/users/{userID}/repositories", response.Handler(h.logger, h.listUserRepositories))
}
