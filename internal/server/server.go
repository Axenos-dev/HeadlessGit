package server

import (
	"context"
	"fmt"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/control"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	webhooksservice "github.com/Axenos-dev/HeadlessGit/internal/services/webhooks"
	"go.uber.org/zap"
)

type Services struct {
	GitBackend gitbackend.Backend

	Repositories   *reposervice.Service
	Users          *usersservice.Service
	Authentication *authservice.Service
	Authorization  *permsservice.Service
	Webhooks       *webhooksservice.Service
	LFS            *lfsservice.Service
	DB             *db.DB
}

// clean up expired tokens every hour
const tokenGCInterval = time.Hour

// number of workers which going to handle webhooks
const webhookWorkers = 3

type server struct {
	cfg    config.ServerConfig
	logger *zap.Logger

	auth     *authservice.Service
	webhooks *webhooksservice.Service

	control *control.Server
	git     *git.Server
}

func NewServer(
	logger *zap.Logger,
	cfg config.ServerConfig,
	svc Services,
) *server {
	return &server{
		cfg:      cfg,
		logger:   logger,
		auth:     svc.Authentication,
		webhooks: svc.Webhooks,
		control: control.NewServer(logger.With(zap.String("component", "control")), control.Services{
			Repositories:   svc.Repositories,
			Authentication: svc.Authentication,
			Authorization:  svc.Authorization,
			Users:          svc.Users,
			Webhooks:       svc.Webhooks,
			Health:         svc.DB,
		}),
		git: git.NewServer(logger.With(zap.String("component", "git")), cfg.HostKeyPath, git.Services{
			Repositories:   svc.Repositories,
			Authentication: svc.Authentication,
			Authorization:  svc.Authorization,
			Backend:        svc.GitBackend,
			LFS:            svc.LFS,
			Webhooks:       svc.Webhooks,
		}),
	}
}

func (s *server) Run(ctx context.Context) error {
	errCh := make(chan error, 3)

	// clean up expired tokens
	go s.auth.RunExpiredTokenGC(ctx, tokenGCInterval)

	// handle webhooks
	// it runs N goroutines for us, so dont Start in goroutine
	s.webhooks.Start(ctx, webhookWorkers)

	go func() {
		errCh <- s.control.Run(ctx, fmt.Sprintf(":%d", s.cfg.ControlPort))
	}()
	go func() {
		errCh <- s.git.RunHTTP(ctx, fmt.Sprintf(":%d", s.cfg.GitHTTPPort))
	}()
	go func() {
		errCh <- s.git.RunSSH(ctx, fmt.Sprintf(":%d", s.cfg.GitSSHPort))
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}
