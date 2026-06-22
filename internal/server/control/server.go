package control

import (
	"net/http"

	repohandlers "github.com/Axenos-dev/HeadlessGit/internal/server/control/repositories"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type Server struct {
	logger *zap.Logger
	repos  repohandlers.RepositoryManager
}

func NewServer(logger *zap.Logger, repos repohandlers.RepositoryManager) *Server {
	return &Server{
		logger: logger,
		repos:  repos,
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
}
