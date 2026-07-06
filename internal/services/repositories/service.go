package repositories

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/archive"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
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
	ListTree(ctx context.Context, storagePath, rev, treePath string) (gitbackend.TreeListing, error)
	ResolveCommit(ctx context.Context, storagePath, rev string) (string, error)
	ArchiveTar(ctx context.Context, storagePath, rev string, out io.Writer) (string, error)
	StatBlob(ctx context.Context, storagePath, rev, treePath string) (gitbackend.BlobInfo, error)
	ReadBlob(ctx context.Context, storagePath, blobSHA string, out io.Writer) error
}

type LFSObjects interface {
	GetObject(ctx context.Context, repo domain.Repository, oid string) (io.ReadCloser, int64, error)
}

type Service struct {
	logger   *zap.Logger
	registry Registry
	storage  RepositoryStorage
	lfs      LFSObjects
}

func NewService(logger *zap.Logger, registry Registry, storage RepositoryStorage, lfs LFSObjects) *Service {
	return &Service{
		logger:   logger,
		registry: registry,
		storage:  storage,
		lfs:      lfs,
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

func (s *Service) Contents(ctx context.Context, repositoryID int64, ref, treePath string) (domain.RepositoryContents, error) {
	repo, err := s.registry.GetRepository(ctx, repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RepositoryContents{}, ErrRepositoryNotFound
	}
	if err != nil {
		return domain.RepositoryContents{}, err
	}

	listing, err := s.storage.ListTree(ctx, repo.StoragePath, ref, treePath)
	switch {
	case errors.Is(err, gitbackend.ErrInvalidRev):
		return domain.RepositoryContents{}, ErrInvalidRef
	case errors.Is(err, gitbackend.ErrInvalidPath):
		return domain.RepositoryContents{}, ErrInvalidPath
	case errors.Is(err, gitbackend.ErrRevNotFound):
		return domain.RepositoryContents{}, ErrRefNotFound
	case errors.Is(err, gitbackend.ErrPathNotFound):
		return domain.RepositoryContents{}, ErrPathNotFound
	case err != nil:
		return domain.RepositoryContents{}, err
	}

	if ref == "" {
		ref = "HEAD"
	}
	return toContents(ref, treePath, listing), nil
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

func (s *Service) PrepareArchive(ctx context.Context, repositoryID int64, ref, format string, includeLFS bool) (domain.ArchiveRequest, error) {
	f, ok := domain.ParseArchiveFormat(format)
	if !ok {
		return domain.ArchiveRequest{}, ErrUnsupportedFormat
	}
	if includeLFS && s.lfs == nil {
		return domain.ArchiveRequest{}, ErrLFSNotEnabled
	}

	repo, err := s.Get(ctx, repositoryID)
	if err != nil {
		return domain.ArchiveRequest{}, err
	}

	sha, err := s.storage.ResolveCommit(ctx, repo.StoragePath, ref)
	switch {
	case errors.Is(err, gitbackend.ErrInvalidRev):
		return domain.ArchiveRequest{}, ErrInvalidRef
	case errors.Is(err, gitbackend.ErrRevNotFound):
		return domain.ArchiveRequest{}, ErrRefNotFound
	case err != nil:
		return domain.ArchiveRequest{}, err
	}

	return domain.ArchiveRequest{
		Repository: repo,
		CommitSHA:  sha,
		Format:     f,
		IncludeLFS: includeLFS,
	}, nil
}

func (s *Service) StreamArchive(ctx context.Context, req domain.ArchiveRequest, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// create pipe and stream git archive to writer
	pr, pw := io.Pipe()
	go func() {
		_, err := s.storage.ArchiveTar(ctx, req.Repository.StoragePath, req.CommitSHA, pw)
		pw.CloseWithError(err)
	}()

	// create archive endocer depending on the archive type, connect it to output writer
	var enc archive.Encoder
	if req.Format == domain.ArchiveFormatTarGz {
		enc = archive.NewTarGzEncoder(out)
	} else {
		enc = archive.NewZipEncoder(out)
	}

	// define a spefic smudge function for LFS objects
	// to translate LFS pointers into a real blobs
	var smudge archive.SmudgeFunc
	if req.IncludeLFS {
		smudge = func(oid string) (io.ReadCloser, int64, error) {
			rc, size, err := s.lfs.GetObject(ctx, req.Repository, oid)
			if err != nil {
				s.logger.Warn("lfs object unavailable for archive, keeping pointer",
					zap.Int64("repository_id", req.Repository.ID),
					zap.String("oid", oid),
					zap.Error(err),
				)
			}
			return rc, size, err
		}
	}

	prefix := fmt.Sprintf("%s-%s/", req.Repository.RepositoryName, domain.ShortSHA(req.CommitSHA))
	return archive.Transform(pr, prefix, smudge, enc)
}

func (s *Service) PrepareBlob(ctx context.Context, repositoryID int64, ref, treePath string, includeLFS bool) (domain.BlobRequest, error) {
	if includeLFS && s.lfs == nil {
		return domain.BlobRequest{}, ErrLFSNotEnabled
	}

	repo, err := s.Get(ctx, repositoryID)
	if err != nil {
		return domain.BlobRequest{}, err
	}

	info, err := s.storage.StatBlob(ctx, repo.StoragePath, ref, treePath)
	switch {
	case errors.Is(err, gitbackend.ErrInvalidRev):
		return domain.BlobRequest{}, ErrInvalidRef
	case errors.Is(err, gitbackend.ErrInvalidPath):
		return domain.BlobRequest{}, ErrInvalidPath
	case errors.Is(err, gitbackend.ErrRevNotFound):
		return domain.BlobRequest{}, ErrRefNotFound
	case errors.Is(err, gitbackend.ErrPathNotFound):
		return domain.BlobRequest{}, ErrPathNotFound
	case errors.Is(err, gitbackend.ErrNotABlob):
		return domain.BlobRequest{}, ErrNotAFile
	case err != nil:
		return domain.BlobRequest{}, err
	}

	req := domain.BlobRequest{
		Repository: repo,
		CommitSHA:  info.CommitSHA,
		BlobSHA:    info.BlobSHA,
		Path:       treePath,
		Size:       info.Size,
	}

	// check pointer-sized blobs
	if includeLFS && info.Size <= domain.LFSPointerMaxSize {
		// read it
		var buf bytes.Buffer
		if err := s.storage.ReadBlob(ctx, repo.StoragePath, info.BlobSHA, &buf); err != nil {
			return domain.BlobRequest{}, err
		}
		// then parse to see if its a pointer
		if ptr, ok := domain.ParseLFSPointer(buf.Bytes()); ok {
			// but the oid is repo content and untrusted
			// so to be safe, we would pull it from dedicated lfs service with respect to repoID
			rc, size, err := s.lfs.GetObject(ctx, repo, ptr.OID)
			if err != nil {
				return domain.BlobRequest{}, ErrLFSObjectNotFound
			}
			rc.Close()

			req.LFSOID = ptr.OID
			req.Size = size
		}
	}

	return req, nil
}

func (s *Service) StreamBlob(ctx context.Context, req domain.BlobRequest, out io.Writer) error {
	if req.LFSOID != "" {
		rc, _, err := s.lfs.GetObject(ctx, req.Repository, req.LFSOID)
		if err != nil {
			return err
		}
		defer rc.Close()

		_, err = io.Copy(out, rc)
		return err
	}
	return s.storage.ReadBlob(ctx, req.Repository.StoragePath, req.BlobSHA, out)
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

func toContents(ref, treePath string, listing gitbackend.TreeListing) domain.RepositoryContents {
	entries := make([]domain.TreeEntry, len(listing.Entries))
	for i, e := range listing.Entries {
		entries[i] = domain.TreeEntry{
			Name: path.Base(e.Path),
			Path: e.Path,
			Type: domain.TreeEntryTypeFromMode(e.Mode),
			Mode: e.Mode,
			SHA:  e.SHA,
			Size: e.Size,
		}
	}
	return domain.RepositoryContents{
		Ref:       ref,
		CommitSHA: listing.CommitSHA,
		Path:      treePath,
		Entries:   entries,
		Truncated: listing.Truncated,
	}
}
