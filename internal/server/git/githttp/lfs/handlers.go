package lfs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/audit"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const contentType = "application/vnd.git-lfs+json"

type RepositoryResolver interface {
	GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error)
}

type Authorizer interface {
	Authorize(ctx context.Context, account *domain.Account, repo domain.Repository, required domain.Role) error
}

type Service interface {
	Batch(ctx context.Context, repo domain.Repository, namespace string, op domain.LFSOperation, uploaderID int64, objects []domain.LFSPointer) ([]domain.LFSObjectResponse, error)
	Verify(ctx context.Context, repo domain.Repository, oid string, size int64) error
	GetObject(ctx context.Context, repo domain.Repository, oid string) (io.ReadCloser, int64, error)
	PutObject(ctx context.Context, repo domain.Repository, oid string, size int64, r io.Reader) error
}

type Handlers struct {
	logger   *zap.Logger
	resolver RepositoryResolver
	authz    Authorizer
	service  Service
}

func NewHandlers(logger *zap.Logger, resolver RepositoryResolver, authz Authorizer, service Service) *Handlers {
	return &Handlers{
		logger:   logger,
		resolver: resolver,
		authz:    authz,
		service:  service,
	}
}

func (h *Handlers) RegisterRoutes(r chi.Router) {
	r.Post("/{namespace}/{name}/info/lfs/objects/batch", h.handleBatch)
	r.Post("/{namespace}/{name}/info/lfs/verify", h.handleVerify)

	// used when lfs storage type is disk
	r.Put("/{namespace}/{name}/info/lfs/objects/{oid}", h.handleUpload)
	r.Get("/{namespace}/{name}/info/lfs/objects/{oid}", h.handleDownload)
}

// resolves the repo from the URL and records it on the audit event
func (h *Handlers) resolveRepo(w http.ResponseWriter, r *http.Request, command string) (domain.Repository, bool) {
	namespace := chi.URLParam(r, "namespace")
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")

	repo, err := h.resolver.GetRepositoryByPath(r.Context(), namespace, name)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "repository not found")
		return domain.Repository{}, false
	}

	if e := audit.FromContext(r.Context()); e != nil {
		e.RepoID = repo.ID
		e.Command = command
	}
	return repo, true
}

func (h *Handlers) writeAuthError(w http.ResponseWriter, account *domain.Account) {
	if account == nil {
		// no creds -> tell git to retry with credentials
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		h.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	h.writeError(w, http.StatusForbidden, "forbidden")
}

func (h *Handlers) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, errorResponse{Message: message})
}

func (h *Handlers) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Warn("failed to encode lfs response", zap.Error(err))
	}
}
