package control

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/server/audit"
	permhandlers "github.com/Axenos-dev/HeadlessGit/internal/server/control/permissions"
	repohandlers "github.com/Axenos-dev/HeadlessGit/internal/server/control/repositories"
	userhandlers "github.com/Axenos-dev/HeadlessGit/internal/server/control/users"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// should report whether a backing dependency (the database) is reachable
type HealthChecker interface {
	Health(ctx context.Context) error
}

type Server struct {
	logger *zap.Logger

	repos  *reposervice.Service
	users  *usersservice.Service
	auth   *authservice.Service
	perms  *permsservice.Service
	health HealthChecker
}

func NewServer(logger *zap.Logger, repos *reposervice.Service, users *usersservice.Service, auth *authservice.Service, perms *permsservice.Service, health HealthChecker) *Server {
	return &Server{
		logger: logger,
		repos:  repos,
		users:  users,
		auth:   auth,
		perms:  perms,
		health: health,
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

	s.logger.Info("control api listening", zap.String("addr", addr))

	// ListenAndServe returns ErrServerClosed after a clean Shutdown
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", s.handleHealth)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequestID, middleware.ClientIPFromRemoteAddr)
		r.Use(audit.Middleware(s.logger, "http"))
		r.Use(middleware.Recoverer)
		r.Use(s.requireAdmin) // every control endpoint needs an admin bearer token

		s.registerRoutes(r)
	})

	return r
}

func (s *Server) registerRoutes(r chi.Router) {
	repohandlers.NewHandlers(s.logger, s.repos).RegisterRoutes(r)
	userhandlers.NewHandlers(s.logger, s.users, s.auth).RegisterRoutes(r)
	permhandlers.NewHandlers(s.logger, s.perms).RegisterRoutes(r)
}
