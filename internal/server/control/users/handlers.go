package users

import (
	"context"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type UserManager interface {
	Create(ctx context.Context, info domain.UserInfo) (domain.Account, error)
}

type CredentialManager interface {
	AddSSHKey(ctx context.Context, userID int64, title, publicKey string) (domain.SSHKey, error)
	MintToken(ctx context.Context, userID int64, title string, expiresAt *time.Time) (string, domain.Token, error)
}

type handlers struct {
	logger *zap.Logger
	users  UserManager
	creds  CredentialManager
}

func NewHandlers(logger *zap.Logger, users UserManager, creds CredentialManager) *handlers {
	return &handlers{
		logger: logger,
		users:  users,
		creds:  creds,
	}
}

func (h *handlers) RegisterRoutes(parent chi.Router) {
	parent.Route("/users", func(r chi.Router) {
		r.Post("/", response.Handler(h.logger, h.createUser))
		r.Post("/{userID}/ssh-keys", response.Handler(h.logger, h.addSSHKey))
		r.Post("/{userID}/tokens", response.Handler(h.logger, h.mintToken))
	})
}
