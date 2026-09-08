package lfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/storage"
	"go.uber.org/zap"
)

type uploadRegistry struct {
	Registry
	row gen.LfsObject
}

func (r *uploadRegistry) CreateVerifiedLFSObject(_ context.Context, userID, repositoryID int64, objectID string, sizeBytes int64, storageKey string) (gen.LfsObject, error) {
	r.row = gen.LfsObject{
		UserID:       userID,
		RepositoryID: repositoryID,
		ObjectID:     objectID,
		SizeBytes:    sizeBytes,
		StorageKey:   storageKey,
		Verified:     true,
	}
	return r.row, nil
}

type uploadStorage struct {
	storage.Storage
	key  string
	body []byte
}

func (s *uploadStorage) Put(_ context.Context, key string, _ int64, body io.Reader) error {
	value, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.key = key
	s.body = value
	return nil
}

func (s *uploadStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("upload must not read the stored object")
}

func (s *uploadStorage) Delete(context.Context, string) error {
	return nil
}

func TestUploadStreamsAndRegistersObject(t *testing.T) {
	payload := []byte("large object")
	repo := domain.Repository{ID: 7}
	registry := &uploadRegistry{}
	store := &uploadStorage{}
	service := NewService(zap.NewNop(), registry, store, "https://git.test", []byte("upload-key"))

	auth := domain.UploadAuthorization{Kind: domain.UploadLFS, UploadID: strings.Repeat("a", 64), RepositoryID: repo.ID, UserID: 42, Size: int64(len(payload)), ExpiresAt: time.Now().Add(time.Minute)}
	auth.Signature = auth.Sign([]byte("upload-key"))
	object, err := service.Upload(context.Background(), auth, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}

	wantHash := sha256.Sum256(payload)
	wantOID := hex.EncodeToString(wantHash[:])
	if object.OID != wantOID || object.Size != int64(len(payload)) {
		t.Fatalf("object = %+v", object)
	}
	if !bytes.Equal(store.body, payload) {
		t.Fatalf("stored body = %q", store.body)
	}
	if registry.row.ObjectID != wantOID || registry.row.StorageKey != store.key || !registry.row.Verified {
		t.Fatalf("lfs row = %+v", registry.row)
	}
	if store.key == objectKey(repo.ID, wantOID) {
		t.Fatalf("upload used oid-derived key %q", store.key)
	}
}

func TestUploadAuthorization(t *testing.T) {
	service := NewService(zap.NewNop(), &uploadRegistry{}, &uploadStorage{}, "https://git.test", []byte("upload-key"))
	auth := domain.UploadAuthorization{Kind: domain.UploadLFS, UploadID: strings.Repeat("a", 64), RepositoryID: 7, UserID: 42, Size: 4, ExpiresAt: time.Now().Add(time.Minute)}
	auth.Signature = auth.Sign([]byte("upload-key"))

	t.Run("tampered", func(t *testing.T) {
		auth := auth
		auth.UserID++
		if _, err := service.Upload(context.Background(), auth, bytes.NewReader([]byte("data"))); !errors.Is(err, ErrInvalidUpload) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		auth := auth
		auth.ExpiresAt = time.Unix(1, 0)
		auth.Signature = auth.Sign([]byte("upload-key"))
		if _, err := service.Upload(context.Background(), auth, bytes.NewReader([]byte("data"))); !errors.Is(err, ErrUploadExpired) {
			t.Fatalf("error = %v", err)
		}
	})
}
