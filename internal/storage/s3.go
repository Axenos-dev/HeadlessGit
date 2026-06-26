package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	_ lfs.ObjectStorage = (*S3)(nil)
	_ lfs.Presigner     = (*S3)(nil)
)

// implements both the services/lfs ObjectStorage and Presigner interfaces
type S3 struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewS3(config config.S3Config) (*S3, error) {
	lookup := minio.BucketLookupAuto
	if config.UsePathStyle {
		lookup = minio.BucketLookupPath
	}

	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure:       config.UseSSL,
		Region:       config.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("init s3 client: %w", err)
	}

	return &S3{
		client: client,
		bucket: config.Bucket,
		prefix: strings.Trim(config.KeyPrefix, "/"),
	}, nil
}

func (s *S3) Stat(ctx context.Context, key string) (bool, int64, error) {
	info, err := s.client.StatObject(ctx, s.bucket, s.key(key), minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	return true, info.Size, nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, s.key(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// minio.Object is lazy; surface a missing object as an error now rather than
	// on the first Read so callers get a clean failure.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, err
	}
	return obj, nil
}

func (s *S3) Put(ctx context.Context, key string, size int64, r io.Reader) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.key(key), r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.key(key), minio.RemoveObjectOptions{})
}

func (s *S3) PresignPut(ctx context.Context, key string, _ int64, ttl time.Duration) (string, map[string]string, error) {
	u, err := s.client.PresignedPutObject(ctx, s.bucket, s.key(key), ttl)
	if err != nil {
		return "", nil, err
	}
	return u.String(), nil, nil
}

func (s *S3) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, s.key(key), ttl, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *S3) key(key string) string {
	if s.prefix == "" {
		return key
	}
	return path.Join(s.prefix, key)
}

func isNotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey"
}
