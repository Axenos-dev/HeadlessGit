package smart

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/audit"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type RepositoryResolver interface {
	GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error)
	ListPathPolicies(ctx context.Context, repositoryID int64) ([]domain.PathPolicy, error)
}

type Authorizer interface {
	Authorize(ctx context.Context, account *domain.Account, repo domain.Repository, required domain.Role) error
}

type GitBackend interface {
	AdvertiseRefs(ctx context.Context, storagePath string, svc gitbackend.Service, stdout io.Writer) error
	UploadPack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) error
	ReceivePack(ctx context.Context, storagePath string, stateless bool, hookEnv []string, stdin io.Reader, stdout, stderr io.Writer) ([]gitbackend.RefChange, error)
}

type Dispatcher interface {
	DispatchEvent(ctx context.Context, event domain.RepositoryEvent) error
}

type Handlers struct {
	logger *zap.Logger

	backend    GitBackend
	resolver   RepositoryResolver
	authz      Authorizer
	dispatcher Dispatcher
}

func NewHandlers(logger *zap.Logger, backend GitBackend, resolver RepositoryResolver, authz Authorizer, dispatcher Dispatcher) *Handlers {
	return &Handlers{
		logger:     logger,
		backend:    backend,
		resolver:   resolver,
		authz:      authz,
		dispatcher: dispatcher,
	}
}

func (h *Handlers) RegisterRoutes(r chi.Router) {
	r.Get("/{namespace}/{name}/info/refs", h.infoRefs)
	r.Post("/{namespace}/{name}/git-upload-pack", h.pack(gitbackend.UploadPack))
	r.Post("/{namespace}/{name}/git-receive-pack", h.pack(gitbackend.ReceivePack))
}

// parseService maps the wire service name to a backend service.
func parseService(name string) (gitbackend.Service, bool) {
	switch name {
	case "git-upload-pack":
		return gitbackend.UploadPack, true
	case "git-receive-pack":
		return gitbackend.ReceivePack, true
	default:
		return 0, false
	}
}

func requiredRole(svc gitbackend.Service) domain.Role {
	if svc == gitbackend.ReceivePack {
		return domain.RoleWrite
	}
	return domain.RoleRead
}

// resolve looks up the repo and checks access
// on failure it writes the response and returns ok=false
func (h *Handlers) resolve(w http.ResponseWriter, r *http.Request, svc gitbackend.Service) (domain.Repository, bool) {
	namespace := chi.URLParam(r, "namespace")
	name := strings.TrimSuffix(chi.URLParam(r, "name"), ".git")

	repo, err := h.resolver.GetRepositoryByPath(r.Context(), namespace, name)
	if err != nil {
		http.NotFound(w, r)
		return domain.Repository{}, false
	}

	if e := audit.FromContext(r.Context()); e != nil {
		e.RepoID = repo.ID
		e.Command = svc.Name()
	}

	account := middleware.AccountFromContext(r.Context()) // nil = anonymous
	if err := h.authz.Authorize(r.Context(), account, repo, requiredRole(svc)); err != nil {
		if account == nil {
			// missing creds -> tell git to retry with credentials
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		} else {
			http.Error(w, "forbidden", http.StatusForbidden)
		}
		return domain.Repository{}, false
	}

	return repo, true
}

func (h *Handlers) infoRefs(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	svc, ok := parseService(serviceName)
	if !ok {
		http.Error(w, "service not enabled", http.StatusForbidden)
		return
	}

	repo, ok := h.resolve(w, r, svc)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", serviceName))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	writePktLine(w, "# service="+serviceName+"\n")
	writeFlushPkt(w)

	if err := h.backend.AdvertiseRefs(r.Context(), repo.StoragePath, svc, w); err != nil {
		h.logger.Warn("advertise refs failed", zap.String("service", serviceName), zap.Error(err))
	}
}

func (h *Handlers) pack(svc gitbackend.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo, ok := h.resolve(w, r, svc)
		if !ok {
			return
		}

		body, err := requestBody(r)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		defer body.Close()

		w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-result", svc.Name()))
		w.Header().Set("Cache-Control", "no-cache")

		var stderr strings.Builder
		switch svc {
		case gitbackend.ReceivePack:
			hookEnv, envErr := hookEnv(r.Context(), h.resolver, repo)
			if envErr != nil {
				h.logger.Error("failed to build hook env", zap.Error(envErr))
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			var changes []gitbackend.RefChange
			changes, err = h.backend.ReceivePack(r.Context(), repo.StoragePath, true, hookEnv, body, w, &stderr)
			if err == nil {
				namespace := chi.URLParam(r, "namespace")
				h.dispatchPush(r.Context(), repo, namespace, middleware.AccountFromContext(r.Context()), changes)
			}
		case gitbackend.UploadPack:
			err = h.backend.UploadPack(r.Context(), repo.StoragePath, true, body, w, &stderr)
		}
		if err != nil {
			h.logger.Warn("git pack failed",
				zap.String("service", svc.Name()),
				zap.String("stderr", strings.TrimSpace(stderr.String())),
				zap.Error(err),
			)
		}
	}
}

// loads the repo's path policies, together with server binary path
func hookEnv(ctx context.Context, resolver RepositoryResolver, repo domain.Repository) ([]string, error) {
	policies, err := resolver.ListPathPolicies(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return domain.HookEnv(bin, policies)
}

func (h *Handlers) dispatchPush(ctx context.Context, repo domain.Repository, namespace string, account *domain.Account, changes []gitbackend.RefChange) {
	if h.dispatcher == nil {
		return
	}

	fullName := namespace + "/" + repo.RepositoryName

	for _, c := range changes {
		err := h.dispatcher.DispatchEvent(ctx, domain.RepositoryEvent{
			Event:              "push",
			RepositoryID:       repo.ID,
			RepositoryName:     repo.RepositoryName,
			RepositoryFullName: fullName,
			PusherID:           account.UserID,
			PusherUsername:     account.Username,
			Ref:                c.Ref,
			OldSHA:             c.OldSHA,
			NewSHA:             c.NewSHA,
			Timestamp:          time.Now().UTC(),
		})
		if err != nil {
			h.logger.Warn("failed to enqueue webhook event", zap.String("ref", c.Ref), zap.Error(err))
		}
	}
}

// decompressing request body if client sent gzip-encoded
// (git does this for larger requests)
func requestBody(r *http.Request) (io.ReadCloser, error) {
	if r.Header.Get("Content-Encoding") == "gzip" {
		return gzip.NewReader(r.Body)
	}
	return r.Body, nil
}

// writes a git protocol pkt-line (4-byte hex length prefix) followed by the payload
func writePktLine(w io.Writer, payload string) {
	// length prefix including 4 bytes for prefix
	fmt.Fprintf(w, "%04x%s", len(payload)+4, payload)
}

// writeFlushPkt writes the special "0000" (flush) packet
func writeFlushPkt(w io.Writer) {
	fmt.Fprint(w, "0000")
}
