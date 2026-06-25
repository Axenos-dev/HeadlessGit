package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/gitcmd"
	"github.com/Axenos-dev/HeadlessGit/internal/server/control"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"go.uber.org/zap"
)

// clean up expired tokens every hour
const tokenGCInterval = time.Hour

type server struct {
	cfg config.ServerConfig

	logger  *zap.Logger
	auth    *authservice.Service
	control *control.Server
	git     *git.Server
}

func NewServer(
	logger *zap.Logger,
	cfg config.ServerConfig,
	repos *reposervice.Service,
	users *usersservice.Service,
	auth *authservice.Service,
	perms *permsservice.Service,
	runner *gitcmd.Runner,
	lfs *lfsservice.Service,
) *server {
	return &server{
		cfg:     cfg,
		logger:  logger,
		auth:    auth,
		control: control.NewServer(logger.With(zap.String("component", "control")), repos, users, auth, perms),
		git:     git.NewServer(logger.With(zap.String("component", "git")), cfg.RepoRoot, cfg.HostKeyPath, runner, repos, auth, perms, lfs),
	}
}

func (s *server) Run() error {
	errCh := make(chan error, 3)

	// clean up expired tokens
	go s.auth.RunExpiredTokenGC(context.Background(), tokenGCInterval)

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
