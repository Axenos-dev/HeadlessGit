package repositories

import (
	"context"
	"io"

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
	Contents(ctx context.Context, repositoryID int64, ref, treePath string) (domain.RepositoryContents, error)
	PrepareArchive(ctx context.Context, repositoryID int64, ref, format string, includeLFS bool) (domain.ArchiveRequest, error)
	StreamArchive(ctx context.Context, req domain.ArchiveRequest, out io.Writer) error
	PrepareBlob(ctx context.Context, repositoryID int64, ref, treePath string, includeLFS bool) (domain.BlobRequest, error)
	StreamBlob(ctx context.Context, req domain.BlobRequest, out io.Writer) error
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
		r.Get("/{repositoryID}/contents", response.Handler(h.logger, h.getContents))
		r.Get("/{repositoryID}/archive", response.Handler(h.logger, h.getArchive))
		r.Get("/{repositoryID}/blob", response.Handler(h.logger, h.getBlob))
		r.Put("/{repositoryID}/visibility", response.Handler(h.logger, h.setVisibility))
		r.Delete("/{repositoryID}", response.Handler(h.logger, h.deleteRepository))
	})

	// a user's repositories (by owner)
	parent.Get("/users/{userID}/repositories", response.Handler(h.logger, h.listUserRepositories))
}
