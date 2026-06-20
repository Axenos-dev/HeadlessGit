package server

import (
	"fmt"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/server/control"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git"
	"go.uber.org/zap"
)

type server struct {
	logger  *zap.Logger
	control *control.Server
	git     *git.Server
}

func NewServer(logger *zap.Logger) *server {
	return &server{
		logger:  logger,
		control: control.NewServer(logger.With(zap.String("component", "control"))),
		git:     git.NewServer(logger.With(zap.String("component", "git"))),
	}
}

func (s *server) Run(cfg config.ServerConfig) error {
	errCh := make(chan error, 3)

	go func() {
		errCh <- s.control.Run(fmt.Sprintf(":%d", cfg.ControlPort))
	}()
	go func() {
		errCh <- s.git.RunHTTP(fmt.Sprintf(":%d", cfg.GitHTTPPort))
	}()
	go func() {
		errCh <- s.git.RunSSH(fmt.Sprintf(":%d", cfg.GitSSHPort))
	}()

	return <-errCh
}
