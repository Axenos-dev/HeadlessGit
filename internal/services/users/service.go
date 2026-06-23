package users

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"go.uber.org/zap"
)

type Registry interface {
	CreateUser(ctx context.Context, username, kind string) (gen.User, error)
}

type Service struct {
	logger   *zap.Logger
	registry Registry
}

func NewService(logger *zap.Logger, registry Registry) *Service {
	return &Service{
		logger:   logger,
		registry: registry,
	}
}

func (s *Service) CreateUser(ctx context.Context, userInfo domain.UserInfo) (domain.Account, error) {
	user, err := s.registry.CreateUser(ctx, userInfo.Username, string(userInfo.Kind))
	if err != nil {
		s.logger.Error("failed to create user", zap.Error(err))
		return domain.Account{}, err
	}

	return toDomain(user), nil
}

func toDomain(user gen.User) domain.Account {
	return domain.Account{
		UserID:   user.ID,
		Username: user.Username,
		Kind:     domain.UserKind(user.Kind),
	}
}
