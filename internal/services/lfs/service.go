package lfs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"go.uber.org/zap"
)

// how long a presigned upload/download URL stays valid
const presignTTL = 15 * time.Minute

type Registry interface {
	CreateLFSObject(ctx context.Context, userID, repositoryID int64, objectID string, sizeBytes int64) (gen.LfsObject, error)
	GetLFSObject(ctx context.Context, repositoryID int64, objectID string) (gen.LfsObject, error)
	DeleteLFSObject(ctx context.Context, repositoryID int64, objectID string) error
	SetLFSObjectVerified(ctx context.Context, repositoryID int64, objectID string, verified bool) (gen.LfsObject, error)
}

type ObjectStorage interface {
	Stat(ctx context.Context, key string) (exists bool, size int64, err error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, size int64, r io.Reader) error
	Delete(ctx context.Context, key string) error
}

type Presigner interface {
	PresignPut(ctx context.Context, key string, size int64, ttl time.Duration) (url string, header map[string]string, err error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error)
}

type Service struct {
	logger   *zap.Logger
	registry Registry
	storage  ObjectStorage

	publicURL string
}

func NewService(logger *zap.Logger, registry Registry, storage ObjectStorage, publicURL string) *Service {
	return &Service{
		logger:    logger,
		registry:  registry,
		storage:   storage,
		publicURL: strings.TrimRight(publicURL, "/"),
	}
}

func (s *Service) lfsBase(namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s.git/info/lfs", s.publicURL, namespace, name)
}

func (s *Service) LFSEndpoint(namespace, name string) string {
	return s.lfsBase(namespace, name)
}

func (s *Service) Batch(
	ctx context.Context,
	repo domain.Repository,
	namespace string,
	op domain.LFSOperation,
	uploaderID int64,
	objects []domain.LFSPointer,
) ([]domain.LFSObjectResponse, error) {
	lfsBase := s.lfsBase(namespace, repo.RepositoryName)

	out := make([]domain.LFSObjectResponse, 0, len(objects))
	for _, p := range objects {
		if err := validateOID(p.OID); err != nil {
			out = append(out, domain.LFSObjectResponse{OID: p.OID, Size: p.Size, Error: &domain.LFSObjectError{Code: 422, Message: "invalid object id"}})
			continue
		}

		var (
			resp domain.LFSObjectResponse
			err  error
		)
		switch op {
		case domain.LFSOperationUpload:
			resp, err = s.batchUpload(ctx, repo, lfsBase, uploaderID, p)
		case domain.LFSOperationDownload:
			resp, err = s.batchDownload(ctx, repo, lfsBase, p)
		default:
			return nil, ErrUnsupportedOperation
		}
		if err != nil {
			return nil, err
		}
		out = append(out, resp)
	}
	return out, nil
}

func (s *Service) batchUpload(
	ctx context.Context,
	repo domain.Repository,
	lfsBase string,
	uploaderID int64,
	p domain.LFSPointer,
) (domain.LFSObjectResponse, error) {
	// select potentially existing lfs object
	existing, err := s.registry.GetLFSObject(ctx, repo.ID, p.OID)

	switch {
	case err == nil && existing.Verified:
		// if already stored and confirmed -> no action, client skips the upload
		return domain.LFSObjectResponse{OID: p.OID, Size: p.Size}, nil
	case err == nil:
		// if row exists but never confirmed -> let the client reupload
	case errors.Is(err, sql.ErrNoRows):
		// record the pending upload
		if _, cerr := s.registry.CreateLFSObject(ctx, uploaderID, repo.ID, p.OID, p.Size); cerr != nil && !errors.Is(cerr, sql.ErrNoRows) {
			return domain.LFSObjectResponse{}, cerr
		}
	default:
		return domain.LFSObjectResponse{}, err
	}

	actions, err := s.uploadActions(ctx, repo, lfsBase, p)
	if err != nil {
		return domain.LFSObjectResponse{}, err
	}
	return domain.LFSObjectResponse{OID: p.OID, Size: p.Size, Actions: actions}, nil
}

func (s *Service) batchDownload(ctx context.Context, repo domain.Repository, lfsBase string, p domain.LFSPointer) (domain.LFSObjectResponse, error) {
	row, err := s.registry.GetLFSObject(ctx, repo.ID, p.OID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !row.Verified) {
		return domain.LFSObjectResponse{OID: p.OID, Size: p.Size, Error: &domain.LFSObjectError{Code: 404, Message: "object does not exist"}}, nil
	}
	if err != nil {
		return domain.LFSObjectResponse{}, err
	}

	action, err := s.downloadAction(ctx, repo, lfsBase, row.ObjectID)
	if err != nil {
		return domain.LFSObjectResponse{}, err
	}
	return domain.LFSObjectResponse{OID: row.ObjectID, Size: row.SizeBytes, Actions: map[string]domain.LFSAction{"download": action}}, nil
}

// builds the upload and verify actions
func (s *Service) uploadActions(ctx context.Context, repo domain.Repository, lfsBase string, p domain.LFSPointer) (map[string]domain.LFSAction, error) {
	verify := domain.LFSAction{Href: lfsBase + "/verify"}

	// if we have presigner
	if pre, ok := s.storage.(Presigner); ok {
		url, header, err := pre.PresignPut(ctx, objectKey(repo.ID, p.OID), p.Size, presignTTL)
		if err != nil {
			return nil, err
		}
		return map[string]domain.LFSAction{
			"upload": {Href: url, Header: header, ExpiresAt: time.Now().Add(presignTTL)},
			"verify": verify,
		}, nil
	}

	// otherwise client PUTs the bytes back to us
	return map[string]domain.LFSAction{
		"upload": {Href: lfsBase + "/objects/" + p.OID},
		"verify": verify,
	}, nil
}

func (s *Service) downloadAction(ctx context.Context, repo domain.Repository, lfsBase, oid string) (domain.LFSAction, error) {
	if pre, ok := s.storage.(Presigner); ok {
		url, err := pre.PresignGet(ctx, objectKey(repo.ID, oid), presignTTL)
		if err != nil {
			return domain.LFSAction{}, err
		}
		return domain.LFSAction{Href: url, ExpiresAt: time.Now().Add(presignTTL)}, nil
	}

	return domain.LFSAction{Href: lfsBase + "/objects/" + oid}, nil
}

// flips "verified" to true, if object exists in storage
func (s *Service) Verify(ctx context.Context, repo domain.Repository, oid string, size int64) error {
	if err := validateOID(oid); err != nil {
		return err
	}

	exists, actual, err := s.storage.Stat(ctx, objectKey(repo.ID, oid))
	if err != nil {
		return err
	}
	if !exists || actual != size {
		return ErrObjectNotFound
	}

	_, err = s.registry.SetLFSObjectVerified(ctx, repo.ID, oid, true)
	return err
}

// streams object for download (if there is no presigner)
func (s *Service) GetObject(ctx context.Context, repo domain.Repository, oid string) (io.ReadCloser, int64, error) {
	if err := validateOID(oid); err != nil {
		return nil, 0, err
	}

	row, err := s.registry.GetLFSObject(ctx, repo.ID, oid)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !row.Verified) {
		return nil, 0, ErrObjectNotFound
	}
	if err != nil {
		return nil, 0, err
	}

	rc, err := s.storage.Get(ctx, objectKey(repo.ID, oid))
	if err != nil {
		return nil, 0, err
	}
	return rc, row.SizeBytes, nil
}

// streams object for an upload (if there is no presigner)
func (s *Service) PutObject(ctx context.Context, repo domain.Repository, oid string, size int64, r io.Reader) error {
	if err := validateOID(oid); err != nil {
		return err
	}

	key := objectKey(repo.ID, oid)
	hasher := sha256.New()
	counter := &countingReader{r: io.TeeReader(r, hasher)}

	if err := s.storage.Put(ctx, key, size, counter); err != nil {
		return err
	}

	if counter.n != size || hex.EncodeToString(hasher.Sum(nil)) != oid {
		if derr := s.storage.Delete(ctx, key); derr != nil {
			s.logger.Warn("failed to remove corrupt lfs object", zap.String("oid", oid), zap.Error(derr))
		}
		return ErrObjectMismatch
	}

	_, err := s.registry.SetLFSObjectVerified(ctx, repo.ID, oid, true)
	return err
}

// {repo_id}/ab/cd/<oid>
func objectKey(repoID int64, oid string) string {
	return fmt.Sprintf("%d/%s/%s/%s", repoID, oid[0:2], oid[2:4], oid)
}

// enforces a 64 char lowercase hex sha256
func validateOID(oid string) error {
	if len(oid) != 64 {
		return ErrInvalidOID
	}
	for _, c := range oid {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return ErrInvalidOID
		}
	}
	return nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
