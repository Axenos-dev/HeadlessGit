package control

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type Server struct {
	logger *zap.Logger
	// services here then
}

func NewServer(logger *zap.Logger) *Server {
	return &Server{
		logger: logger,
	}
}

func (s *Server) Run(addr string) error {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.ClientIPFromRemoteAddr, middleware.Recoverer)

	s.registerRoutes(r)

	s.logger.Info("control api listening", zap.String("addr", addr))
	return http.ListenAndServe(addr, r)
}

func (s *Server) registerRoutes(control chi.Router) {
	// control.Route("/repos", ...)
}
