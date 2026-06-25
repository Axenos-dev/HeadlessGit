package githttp

import (
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/lfs"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/smart"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type Server struct {
	logger *zap.Logger

	repoRoot string

	repos *reposervice.Service
	auth  *authservice.Service
	perms *permsservice.Service
	lfs   *lfsservice.Service // nil if disabled
}

func NewServer(logger *zap.Logger, repoRoot string, repos *reposervice.Service, auth *authservice.Service, perms *permsservice.Service, lfs *lfsservice.Service) *Server {
	return &Server{
		repoRoot: repoRoot,
		logger:   logger,
		auth:     auth,
		repos:    repos,
		perms:    perms,
		lfs:      lfs,
	}
}

func (s *Server) Run(addr string) error {
	s.logger.Info("git http listening", zap.String("addr", addr))
	return http.ListenAndServe(addr, s.Handler())
}

// Handler builds the configured chi router for the git HTTP transport.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID, chimiddleware.ClientIPFromRemoteAddr, chimiddleware.Recoverer)
	r.Use(middleware.WithAccount(s.auth))

	s.registerRoutes(r)
	return r
}

func (s *Server) registerRoutes(r chi.Router) {
	smart.NewHandlers(s.logger, s.repoRoot, s.repos, s.perms).RegisterRoutes(r)

	// register LFS handlers if lfs service is provided
	if s.lfs != nil {
		lfs.NewHandlers(s.logger, s.repos, s.perms, s.lfs).RegisterRoutes(r)
	}
}
