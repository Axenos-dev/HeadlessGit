package server

import (
	"fmt"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/gitcmd"
	"github.com/Axenos-dev/HeadlessGit/internal/server/control"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"go.uber.org/zap"
)

type server struct {
	cfg config.ServerConfig

	logger  *zap.Logger
	control *control.Server
	git     *git.Server
}

func NewServer(
	logger *zap.Logger,
	cfg config.ServerConfig,
	repos *reposervice.Service,
	runner *gitcmd.Runner,
) *server {
	return &server{
		cfg:     cfg,
		logger:  logger,
		control: control.NewServer(logger.With(zap.String("component", "control")), repos),
		git:     git.NewServer(logger.With(zap.String("component", "git")), cfg.RepoRoot, cfg.HostKeyPath, runner, repos),
	}
}

func (s *server) Run() error {
	errCh := make(chan error, 3)

	go func() {
		errCh <- s.control.Run(fmt.Sprintf(":%d", s.cfg.ControlPort))
	}()
	go func() {
		errCh <- s.git.RunHTTP(fmt.Sprintf(":%d", s.cfg.GitHTTPPort))
	}()
	go func() {
		errCh <- s.git.RunSSH(fmt.Sprintf(":%d", s.cfg.GitSSHPort))
	}()

	return <-errCh
}
