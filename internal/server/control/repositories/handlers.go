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
	GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error)
	Delete(ctx context.Context, repositoryID int64) error
	SetVisibility(ctx context.Context, repositoryID int64, visibility domain.RepoVisibility) (domain.Repository, error)
	ListByOwner(ctx context.Context, ownerID int64) ([]domain.Repository, error)
	Tree(ctx context.Context, repositoryID int64, ref, treePath string, opts domain.TreeOptions) (domain.RepositoryTree, error)
	Diff(ctx context.Context, repositoryID int64, base, head string) (domain.RepositoryDiff, error)
	GetCommit(ctx context.Context, repositoryID int64, sha string) (domain.CommitDetails, error)
	PrepareArchive(ctx context.Context, repositoryID int64, ref, format string, includeLFS bool, prefix *string) (domain.ArchiveRequest, error)
	StreamArchive(ctx context.Context, req domain.ArchiveRequest, out io.Writer) error
	GetFile(ctx context.Context, repositoryID int64, blobSHA string) (domain.FileInfo, error)
	PrepareFile(ctx context.Context, repositoryID int64, blobSHA string) (domain.FileRequest, error)
	StreamFile(ctx context.Context, req domain.FileRequest, out io.Writer) error
	CreateUpload(ctx context.Context, repositoryID, userID, size int64) (domain.UploadTarget, error)
	Commit(ctx context.Context, repositoryID int64, req domain.CommitRequest) (domain.CommitResult, error)
	ListPathPolicies(ctx context.Context, repositoryID int64) ([]domain.PathPolicy, error)
	AddPathPolicy(ctx context.Context, repositoryID int64, pattern, reason string) (domain.PathPolicy, error)
	RemovePathPolicy(ctx context.Context, repositoryID, policyID int64) error
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
		r.Get("/by-path/{namespace}/{name}", response.Handler(h.logger, h.getRepositoryByPath))
		r.Post("/{repositoryID}/uploads", response.Handler(h.logger, h.createUpload))
		r.Post("/{repositoryID}/commits", response.Handler(h.logger, h.createCommit))
		r.Get("/{repositoryID}/commits/{sha}", response.Handler(h.logger, h.getCommit))
		r.Get("/{repositoryID}", response.Handler(h.logger, h.getRepository))
		r.Get("/{repositoryID}/tree", response.Handler(h.logger, h.getTree))
		r.Get("/{repositoryID}/diff", response.Handler(h.logger, h.getDiff))
		r.Get("/{repositoryID}/archive", response.Handler(h.logger, h.getArchive))
		r.Get("/{repositoryID}/files/{blobSHA}/content", response.Handler(h.logger, h.getFileContent))
		r.Get("/{repositoryID}/files/{blobSHA}", response.Handler(h.logger, h.getFile))
		r.Get("/{repositoryID}/path-policies", response.Handler(h.logger, h.listPathPolicies))
		r.Post("/{repositoryID}/path-policies", response.Handler(h.logger, h.addPathPolicy))
		r.Delete("/{repositoryID}/path-policies/{policyID}", response.Handler(h.logger, h.removePathPolicy))
		r.Put("/{repositoryID}/visibility", response.Handler(h.logger, h.setVisibility))
		r.Delete("/{repositoryID}", response.Handler(h.logger, h.deleteRepository))
	})

	// a user's repositories (by owner)
	parent.Get("/users/{userID}/repositories", response.Handler(h.logger, h.listUserRepositories))
}
