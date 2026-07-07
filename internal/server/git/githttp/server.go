package githttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/audit"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/lfs"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/smart"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	webhooksservice "github.com/Axenos-dev/HeadlessGit/internal/services/webhooks"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type Services struct {
	Repositories   *reposervice.Service
	Authentication *authservice.Service
	Authorization  *permsservice.Service
	Backend        gitbackend.Backend
	LFS            *lfsservice.Service
	Dispatcher     *webhooksservice.Service
}

type Server struct {
	logger *zap.Logger

	backend gitbackend.Backend

	dispatcher *webhooksservice.Service
	repos      *reposervice.Service
	auth       *authservice.Service
	perms      *permsservice.Service
	lfs        *lfsservice.Service // nil if disabled
}

func NewServer(logger *zap.Logger, svc Services) *Server {
	return &Server{
		logger:     logger,
		auth:       svc.Authentication,
		backend:    svc.Backend,
		dispatcher: svc.Dispatcher,
		repos:      svc.Repositories,
		perms:      svc.Authorization,
		lfs:        svc.LFS,
	}
}

func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler()}

	// shut down when ctx is cancelled
	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info("git http listening", zap.String("addr", addr))

	// ListenAndServe returns ErrServerClosed after a clean Shutdown
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID, chimiddleware.ClientIPFromRemoteAddr)
	r.Use(audit.Middleware(s.logger, "http"))
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.WithAccount(s.auth))

	s.registerRoutes(r)
	return r
}

func (s *Server) registerRoutes(r chi.Router) {
	var dispatcher smart.Dispatcher
	if s.dispatcher != nil {
		dispatcher = s.dispatcher
	}
	smart.NewHandlers(s.logger, s.backend, s.repos, s.perms, dispatcher).RegisterRoutes(r)

	// register LFS handlers if lfs service is provided
	if s.lfs != nil {
		lfs.NewHandlers(s.logger, s.repos, s.perms, s.lfs).RegisterRoutes(r)
	}
}
