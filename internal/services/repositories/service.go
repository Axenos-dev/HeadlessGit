package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"go.uber.org/zap"
)

type Registry interface {
	GetRepository(ctx context.Context, repositoryID int64) (gen.Repository, error)
	CreateRepository(ctx context.Context, ownerID int64, name, storagePath, visibility string) (gen.Repository, error)
	DeleteRepository(ctx context.Context, repositoryID int64) error
	GetRepositoryByPath(ctx context.Context, namespace, name string) (gen.Repository, error)
	UpdateRepositoryVisibility(ctx context.Context, repositoryID int64, visibility string) (gen.Repository, error)
	ListUserRepositories(ctx context.Context, ownerID int64) ([]gen.Repository, error)
}

type RepositoryStorage interface {
	InitBare(ctx context.Context, storagePath string) error
	Remove(ctx context.Context, storagePath string) error
}

type Service struct {
	logger   *zap.Logger
	registry Registry
	storage  RepositoryStorage
}

func NewService(logger *zap.Logger, registry Registry, storage RepositoryStorage) *Service {
	return &Service{
		logger:   logger,
		registry: registry,
		storage:  storage,
	}
}

func (s *Service) Get(ctx context.Context, repositoryID int64) (domain.Repository, error) {
	repo, err := s.registry.GetRepository(ctx, repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, ErrRepositoryNotFound
	}
	if err != nil {
		return domain.Repository{}, err
	}
	return toDomain(repo), nil
}

func (s *Service) Create(ctx context.Context, ownerID int64, info domain.RepositoryInfo) (domain.Repository, error) {
	if !validRepositoryName(info.RepositoryName) {
		return domain.Repository{}, ErrInvalidRepositoryName
	}

	storagePath := fmt.Sprintf("%d/%s.git", ownerID, info.RepositoryName)

	// insert row first, check if we pass the main constrains
	repo, err := s.registry.CreateRepository(ctx, ownerID, info.RepositoryName, storagePath, string(info.Visibility))
	if err != nil {
		s.logger.Error("failed to create repository", zap.Error(err))
		return domain.Repository{}, err
	}

	// then initiate the bare repo
	if err := s.storage.InitBare(ctx, storagePath); err != nil {
		// roll back the row (just delete) in case of an error
		if delErr := s.registry.DeleteRepository(ctx, repo.ID); delErr != nil {
			s.logger.Error(
				"failed to roll back repository row after init failure",
				zap.Int64("repository_id", repo.ID),
				zap.Error(delErr),
			)
		}
		return domain.Repository{}, err
	}

	return toDomain(repo), nil
}

func (s *Service) Delete(ctx context.Context, repositoryID int64) error {
	// fetch first so we know the storage path and can return a proper not-found
	repo, err := s.registry.GetRepository(ctx, repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRepositoryNotFound
	}
	if err != nil {
		return err
	}

	// first delete the row in db
	if err := s.registry.DeleteRepository(ctx, repositoryID); err != nil {
		return err
	}

	// second, delete the bare repo
	// we dont care if error occurs, as we treat db as main source of truth
	if err := s.storage.Remove(ctx, repo.StoragePath); err != nil {
		s.logger.Error(
			"failed to remove repository directory after delete",
			zap.Int64("repository_id", repositoryID),
			zap.String("storage_path", repo.StoragePath),
			zap.Error(err),
		)
	}

	return nil
}

func (s *Service) SetVisibility(ctx context.Context, repositoryID int64, visibility domain.RepoVisibility) (domain.Repository, error) {
	if visibility != domain.RepoVisibilityPublic && visibility != domain.RepoVisibilityPrivate {
		return domain.Repository{}, ErrInvalidVisibility
	}

	repo, err := s.registry.UpdateRepositoryVisibility(ctx, repositoryID, string(visibility))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, ErrRepositoryNotFound
	}
	if err != nil {
		return domain.Repository{}, err
	}
	return toDomain(repo), nil
}

func (s *Service) ListByOwner(ctx context.Context, ownerID int64) ([]domain.Repository, error) {
	repos, err := s.registry.ListUserRepositories(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Repository, len(repos))
	for i, repo := range repos {
		out[i] = toDomain(repo)
	}
	return out, nil
}

func (s *Service) GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error) {
	repo, err := s.registry.GetRepositoryByPath(ctx, namespace, name)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, ErrRepositoryNotFound
	}
	if err != nil {
		return domain.Repository{}, err
	}
	return toDomain(repo), nil
}

func toDomain(r gen.Repository) domain.Repository {
	repo := domain.Repository{
		ID:             r.ID,
		OwnerID:        r.OwnerID,
		RepositoryName: r.RepositoryName,
		StoragePath:    r.StoragePath,
		Visibility:     domain.RepoVisibility(r.Visibility),
		CreatedAt:      time.UnixMilli(r.CreatedAtUnixMs).UTC(),
	}
	if r.UpdatedAtUnixMs.Valid {
		t := time.UnixMilli(r.UpdatedAtUnixMs.Int64).UTC()
		repo.UpdatedAt = &t
	}
	return repo
}

func validRepositoryName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, "/\\")
}
