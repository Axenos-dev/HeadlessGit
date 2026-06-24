package control

import (
	"net/http"

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

type Server struct {
	logger *zap.Logger

	repos *reposervice.Service
	users *usersservice.Service
	auth  *authservice.Service
	perms *permsservice.Service
}

func NewServer(logger *zap.Logger, repos *reposervice.Service, users *usersservice.Service, auth *authservice.Service, perms *permsservice.Service) *Server {
	return &Server{
		logger: logger,
		repos:  repos,
		users:  users,
		auth:   auth,
		perms:  perms,
	}
}

func (s *Server) Run(addr string) error {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.ClientIPFromRemoteAddr, middleware.Recoverer)

	s.registerRoutes(r)

	s.logger.Info("control api listening", zap.String("addr", addr))
	return http.ListenAndServe(addr, r)
}

func (s *Server) registerRoutes(r chi.Router) {
	repohandlers.NewHandlers(s.logger, s.repos).RegisterRoutes(r)
	userhandlers.NewHandlers(s.logger, s.users, s.auth).RegisterRoutes(r)
	permhandlers.NewHandlers(s.logger, s.perms).RegisterRoutes(r)
}
