package users

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type UserManager interface {
	Create(ctx context.Context, info domain.UserInfo) (domain.Account, error)
	Get(ctx context.Context, userID int64) (domain.Account, error)
	GetByUsername(ctx context.Context, username string) (domain.Account, error)
}

type CredentialManager interface {
	AddSSHKey(ctx context.Context, userID int64, title, publicKey string) (domain.SSHKey, error)
	ListSSHKeys(ctx context.Context, userID int64) ([]domain.SSHKey, error)
	RemoveSSHKeyByID(ctx context.Context, userID, keyID int64) error

	MintToken(ctx context.Context, userID int64, title string, expiresAt *time.Time) (string, domain.Token, error)
	ListTokens(ctx context.Context, userID int64) ([]domain.Token, error)
	RevokeToken(ctx context.Context, userID, tokenID int64) error
	RevokeAllTokens(ctx context.Context, userID int64) error
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
		r.Get("/by-username/{username}", response.Handler(h.logger, h.getUserByUsername))
		r.Get("/{userID}", response.Handler(h.logger, h.getUser))

		r.Get("/{userID}/ssh-keys", response.Handler(h.logger, h.listSSHKeys))
		r.Post("/{userID}/ssh-keys", response.Handler(h.logger, h.addSSHKey))
		r.Delete("/{userID}/ssh-keys/{keyID}", response.Handler(h.logger, h.deleteSSHKey))

		r.Get("/{userID}/tokens", response.Handler(h.logger, h.listTokens))
		r.Post("/{userID}/tokens", response.Handler(h.logger, h.mintToken))
		r.Delete("/{userID}/tokens", response.Handler(h.logger, h.revokeAllTokens))
		r.Delete("/{userID}/tokens/{tokenID}", response.Handler(h.logger, h.revokeToken))
	})
}

// maps service errors for every route in this group, so one error always gets one response
func (h *handlers) usersError(err error, logMessage string) *response.APIError {
	switch {
	case errors.Is(err, usersservice.ErrUserNotFound):
		return response.NewError(http.StatusNotFound, response.CodeUserNotFound, "user not found")
	case errors.Is(err, usersservice.ErrUserExists):
		return response.NewError(http.StatusConflict, response.CodeUserExists, "user already exists")

	// credentials
	case errors.Is(err, authservice.ErrInvalidSSHKey):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid ssh key")
	case errors.Is(err, authservice.ErrSSHKeyNotFound):
		return response.NewError(http.StatusNotFound, response.CodeSSHKeyNotFound, "ssh key not found")
	case errors.Is(err, authservice.ErrTokenNotFound):
		return response.NewError(http.StatusNotFound, response.CodeTokenNotFound, "token not found")

	default:
		h.logger.Error(logMessage, zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, logMessage)
	}
}
