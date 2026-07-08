package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type Registry interface {
	GetUser(ctx context.Context, userID int64) (gen.User, error)
	CreateUser(ctx context.Context, username, kind string) (gen.User, error)
}

type Service struct {
	registry Registry
}

func NewService(registry Registry) *Service {
	return &Service{
		registry: registry,
	}
}

func (s *Service) Create(ctx context.Context, info domain.UserInfo) (domain.Account, error) {
	user, err := s.registry.CreateUser(ctx, info.Username, string(info.Kind))
	// the insert is "on conflict do nothing returning *" -> on duplicate "no rows"
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, fmt.Errorf("%w: %q", ErrUserExists, info.Username)
	}
	if err != nil {
		return domain.Account{}, err
	}
	return toAccount(user), nil
}

func (s *Service) Get(ctx context.Context, userID int64) (domain.Account, error) {
	user, err := s.registry.GetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, ErrUserNotFound
	}
	if err != nil {
		return domain.Account{}, err
	}
	return toAccount(user), nil
}

func toAccount(u gen.User) domain.Account {
	return domain.Account{
		UserID:   u.ID,
		Username: u.Username,
		Kind:     domain.UserKind(u.Kind),
		IsAdmin:  u.IsAdmin != 0,
	}
}
