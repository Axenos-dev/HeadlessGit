package git

import (
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/gitssh"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type Server struct {
	logger *zap.Logger
	http   *githttp.Handlers
	ssh    *gitssh.Server
}

func NewServer(logger *zap.Logger) *Server {
	return &Server{
		logger: logger,
		http:   githttp.NewHandlers(logger.With(zap.String("transport", "http"))),
		ssh:    gitssh.NewServer(logger.With(zap.String("transport", "ssh"))),
	}
}

func (s *Server) RunHTTP(addr string) error {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.ClientIPFromRemoteAddr, middleware.Recoverer)

	s.http.RegisterRoutes(r)

	s.logger.Info("git http listening", zap.String("addr", addr))
	return http.ListenAndServe(addr, r)
}

func (s *Server) RunSSH(addr string) error {
	return s.ssh.Run(addr)
}
