package smart

import (
	"context"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type RepositoryResolver interface {
	GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error)
}

type Authorizer interface {
	Authorize(ctx context.Context, account *domain.Account, repo domain.Repository, required domain.Role) error
}

type Handlers struct {
	logger *zap.Logger

	gitPath  string
	repoRoot string

	resolver RepositoryResolver
	authz    Authorizer
}

func NewHandlers(logger *zap.Logger, repoRoot string, resolver RepositoryResolver, authz Authorizer) *Handlers {
	// look for git path in system
	gitPath, _ := exec.LookPath("git")

	return &Handlers{
		logger:   logger,
		repoRoot: repoRoot,
		resolver: resolver,
		authz:    authz,
		gitPath:  gitPath,
	}
}

func (h *Handlers) RegisterRoutes(r chi.Router) {
	r.Get("/{namespace}/{name}/info/refs", h.serve("/info/refs"))
	r.Post("/{namespace}/{name}/git-upload-pack", h.serve("/git-upload-pack"))
	r.Post("/{namespace}/{name}/git-receive-pack", h.serve("/git-receive-pack"))
}

func (h *Handlers) serve(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")
		name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")

		repo, err := h.resolver.GetRepositoryByPath(r.Context(), namespace, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		account := middleware.AccountFromContext(r.Context()) // nil = anonymous
		if err := h.authz.Authorize(r.Context(), account, repo, requiredRole(r, service)); err != nil {
			if account == nil {
				// missing creds -> tell git to retry with credentials
				w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			} else {
				http.Error(w, "forbidden", http.StatusForbidden)
			}
			return
		}

		// identity recorded by git
		remoteUser := "anonymous"
		if account != nil { // anonymous if unauthenticated
			remoteUser = account.Username
		}

		// basically re-route {namespace}/{name} to storage path
		r.URL.Path = "/" + repo.StoragePath + service
		h.backend(remoteUser).ServeHTTP(w, r)
	}
}

// map the requested git service to the access level it needs
func requiredRole(r *http.Request, service string) domain.Role {
	svc := service
	if service == "/info/refs" {
		svc = "/" + r.URL.Query().Get("service")
	}
	if svc == "/git-receive-pack" {
		return domain.RoleWrite
	}
	return domain.RoleRead
}

func (h *Handlers) backend(remoteUser string) http.Handler {
	root, _ := filepath.Abs(h.repoRoot)

	return &cgi.Handler{
		Path: h.gitPath,
		Args: []string{"http-backend"},
		Dir:  root,
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
			"REMOTE_USER=" + remoteUser,
			"PATH=" + os.Getenv("PATH"),
		},
	}
}
