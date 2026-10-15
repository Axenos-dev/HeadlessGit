package repositories

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
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
	ListBranches(ctx context.Context, repositoryID int64) (domain.BranchListing, error)
	CreateBranch(ctx context.Context, repositoryID int64, req domain.CreateBranchRequest) (domain.Branch, error)
	DeleteBranch(ctx context.Context, repositoryID int64, req domain.DeleteBranchRequest) error
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
		r.Get("/{repositoryID}/branches", response.Handler(h.logger, h.listBranches))
		r.Post("/{repositoryID}/branches", response.Handler(h.logger, h.createBranch))
		// catch-all, since branch names keep their slashes (feature/x)
		r.Delete("/{repositoryID}/branches/*", response.Handler(h.logger, h.deleteBranch))
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

// maps service errors for every route in this group, so one error always gets one response.
// a route that needs a different answer checks for that error before calling this
func (h *handlers) repositoriesError(err error, logMessage string) *response.APIError {
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrRepositoryExists):
		return response.NewError(http.StatusConflict, response.CodeRepositoryExists, "repository already exists")
	case errors.Is(err, reposervice.ErrInvalidRepositoryName):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository name")
	case errors.Is(err, reposervice.ErrInvalidVisibility):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid visibility")

	// refs, commits, and paths
	case errors.Is(err, reposervice.ErrRefNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRefNotFound, "ref not found")
	case errors.Is(err, reposervice.ErrInvalidRef):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid ref")
	case errors.Is(err, reposervice.ErrCommitNotFound):
		return response.NewError(http.StatusNotFound, response.CodeCommitNotFound, "commit not found")
	case errors.Is(err, reposervice.ErrInvalidCommitSHA):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid commit sha")
	case errors.Is(err, reposervice.ErrPathNotFound):
		return response.NewError(http.StatusNotFound, response.CodePathNotFound, "path not found")
	case errors.Is(err, reposervice.ErrInvalidPath):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid path")
	case errors.Is(err, reposervice.ErrPathConflict):
		return response.NewError(http.StatusConflict, response.CodePathConflict, "path already exists")

	// files, uploads, and lfs
	case errors.Is(err, reposervice.ErrUnknownBlob):
		return response.NewError(http.StatusNotFound, response.CodeUnknownBlob, "blob not found")
	case errors.Is(err, reposervice.ErrNotAFile), errors.Is(err, reposervice.ErrInvalidBlobSHA):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	case errors.Is(err, reposervice.ErrBlobTooLarge):
		return response.NewError(http.StatusRequestEntityTooLarge, response.CodeInvalidRequest, err.Error())
	case errors.Is(err, reposervice.ErrInvalidUpload):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid upload request")
	case errors.Is(err, reposervice.ErrUploadUnavailable):
		return response.NewError(http.StatusServiceUnavailable, response.CodeLFSUnavailable, "uploads are unavailable")
	case errors.Is(err, reposervice.ErrLFSNotEnabled):
		return response.NewError(http.StatusServiceUnavailable, response.CodeLFSUnavailable, "lfs is not enabled")
	case errors.Is(err, reposervice.ErrLFSObjectNotFound):
		return response.NewError(http.StatusNotFound, response.CodeLFSObjectNotFound, "lfs object not found")

	// archives
	case errors.Is(err, reposervice.ErrUnsupportedFormat):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "unsupported archive format")
	case errors.Is(err, reposervice.ErrInvalidArchivePrefix):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid archive prefix")

	// commits and branches
	case errors.Is(err, reposervice.ErrInvalidBranch):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid branch name")
	case errors.Is(err, reposervice.ErrInvalidCommitOps):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	case errors.Is(err, reposervice.ErrHeadMismatch):
		return response.NewError(http.StatusConflict, response.CodeHeadMismatch, "branch head does not match the expected sha")
	case errors.Is(err, reposervice.ErrNothingToCommit):
		return response.NewError(http.StatusUnprocessableEntity, response.CodeNothingToCommit, "operations produce no change")
	case errors.Is(err, reposervice.ErrPathBlocked):
		return response.NewError(http.StatusUnprocessableEntity, response.CodePathBlocked, err.Error())
	case errors.Is(err, reposervice.ErrBranchExists):
		return response.NewError(http.StatusConflict, response.CodeBranchExists, "branch already exists or its name conflicts with an existing branch")
	case errors.Is(err, reposervice.ErrDefaultBranch):
		return response.NewError(http.StatusUnprocessableEntity, response.CodeDefaultBranch, "the default branch cannot be deleted")

	// path policies
	case errors.Is(err, reposervice.ErrInvalidPathPattern):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid path pattern")
	case errors.Is(err, reposervice.ErrPathPolicyExists):
		return response.NewError(http.StatusConflict, response.CodePathPolicyExists, "path policy already exists")

	default:
		h.logger.Error(logMessage, zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, logMessage)
	}
}
