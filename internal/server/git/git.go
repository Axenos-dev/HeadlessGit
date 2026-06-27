package git

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/gitcmd"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/gitssh"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"go.uber.org/zap"
)

type Server struct {
	logger *zap.Logger
	http   *githttp.Server
	ssh    *gitssh.Server
}

func NewServer(logger *zap.Logger, repoRoot, hostKeyPath string, runner *gitcmd.Runner, repos *reposervice.Service, auth *authservice.Service, perms *permsservice.Service, lfs *lfsservice.Service) *Server {
	httpLogger := logger.With(zap.String("transport", "http"))

	// for git over ssh
	var lfsEndpoints gitssh.LFSEndpoints
	if lfs != nil {
		lfsEndpoints = lfs
	}

	return &Server{
		logger: logger,
		http:   githttp.NewServer(httpLogger, repoRoot, repos, auth, perms, lfs),
		ssh:    gitssh.NewServer(logger.With(zap.String("transport", "ssh")), hostKeyPath, runner, repos, auth, perms, auth, lfsEndpoints),
	}
}

func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	return s.http.Run(ctx, addr)
}

func (s *Server) RunSSH(ctx context.Context, addr string) error {
	return s.ssh.Run(ctx, addr)
}
