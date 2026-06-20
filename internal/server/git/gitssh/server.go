package gitssh

import "go.uber.org/zap"

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
	s.logger.Info("git ssh listening (not implemented yet)", zap.String("addr", addr))
	select {} // block until the ssh server is implemented
}
