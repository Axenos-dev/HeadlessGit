package git

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/gitssh"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	webhooksservice "github.com/Axenos-dev/HeadlessGit/internal/services/webhooks"
	"go.uber.org/zap"
)

type Services struct {
	Repositories   *reposervice.Service
	Authentication *authservice.Service
	Authorization  *permsservice.Service
	Backend        gitbackend.Backend
	LFS            *lfsservice.Service
	Webhooks       *webhooksservice.Service
}

type Server struct {
	logger *zap.Logger
	http   *githttp.Server
	ssh    *gitssh.Server
}

func NewServer(logger *zap.Logger, hostKeyPath string, svc Services) *Server {
	httpLogger := logger.With(zap.String("transport", "http"))

	// for git over ssh
	var lfsEndpoints gitssh.LFSEndpoints
	if svc.LFS != nil {
		lfsEndpoints = svc.LFS
	}

	return &Server{
		logger: logger,
		http: githttp.NewServer(httpLogger, githttp.Services{
			Repositories:   svc.Repositories,
			Authentication: svc.Authentication,
			Authorization:  svc.Authorization,
			Backend:        svc.Backend,
			LFS:            svc.LFS,
			Dispatcher:     svc.Webhooks,
		}),
		ssh: gitssh.NewServer(logger.With(zap.String("transport", "ssh")), hostKeyPath, gitssh.Services{
			Backend:        svc.Backend,
			Resolver:       svc.Repositories,
			Authentication: svc.Authentication,
			Minter:         svc.Authentication,
			Authorization:  svc.Authorization,
			LFS:            lfsEndpoints,
			Dispatcher:     svc.Webhooks,
		}),
	}
}

func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	return s.http.Run(ctx, addr)
}

func (s *Server) RunSSH(ctx context.Context, addr string) error {
	return s.ssh.Run(ctx, addr)
}
