package githttp

import (
	"context"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type RepositoryResolver interface {
	GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error)
}

type Authenticator interface {
	AuthenticateToken(ctx context.Context, rawToken string) (domain.Account, error)
}

type Handlers struct {
	logger   *zap.Logger
	repoRoot string
	resolver RepositoryResolver
	auth     Authenticator
}

// key for context
type contextKey string

const accountKey contextKey = "account"

func NewHandlers(logger *zap.Logger, repoRoot string, resolver RepositoryResolver, auth Authenticator) *Handlers {
	return &Handlers{
		logger:   logger,
		repoRoot: repoRoot,
		resolver: resolver,
		auth:     auth,
	}
}

func (h *Handlers) RegisterRoutes(githttp chi.Router) {
	githttp.Use(h.withAccount)
	githttp.Get("/{namespace}/{name}/info/refs", h.serve("/info/refs"))
	githttp.Post("/{namespace}/{name}/git-upload-pack", h.serve("/git-upload-pack"))
	githttp.Post("/{namespace}/{name}/git-receive-pack", h.serve("/git-receive-pack"))
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

		// basically re-route {namespace}/{name} to storage path
		r.URL.Path = "/" + repo.StoragePath + service
		h.backend().ServeHTTP(w, r)
	}
}

func (h *Handlers) withAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if ok {
			account, err := h.auth.AuthenticateToken(r.Context(), password)
			if err != nil {
				// a token was provided but it's invalid -> reject
				w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			r = r.WithContext(ctxWithAccount(r.Context(), &account))
		}

		// no credentials -> anonymous
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) backend() http.Handler {
	// look for git path in system
	gitPath, _ := exec.LookPath("git")
	root, _ := filepath.Abs(h.repoRoot)

	return &cgi.Handler{
		Path: gitPath,
		Args: []string{"http-backend"},
		Dir:  root,
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
			"REMOTE_USER=anonymous", // no auth yet
			"PATH=" + os.Getenv("PATH"),
		},
	}
}

func ctxWithAccount(ctx context.Context, account *domain.Account) context.Context {
	return context.WithValue(ctx, accountKey, account)
}

// accountFromContext returns the authenticated account, or nil for anonymous.
func accountFromContext(ctx context.Context) *domain.Account {
	account, _ := ctx.Value(accountKey).(*domain.Account)
	return account
}
