package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
)

// TestS3RoundTrip exercises the real S3/R2 backend end to end: the direct
// ObjectStorage methods plus the presigned-URL transfer path that unit tests
// can't reach. It is skipped unless the LFS_S3_* env vars are set, e.g.:
//
//	LFS_S3_BUCKET=my-bucket \
//	LFS_S3_REGION=auto \
//	LFS_S3_ENDPOINT=<account>.r2.cloudflarestorage.com \
//	LFS_S3_ACCESS_KEY_ID=... \
//	LFS_S3_SECRET_ACCESS_KEY=... \
//	go test ./internal/storage/ -run TestS3RoundTrip -v
func TestS3RoundTrip(t *testing.T) {
	opts := s3OptsFromEnv(t)

	store, err := NewS3(opts)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	ctx := context.Background()
	key := "_headlessgit_itest/" + randomHex(t, 16)
	payload := []byte("hello from headlessgit s3 round-trip " + randomHex(t, 8))

	// make sure we don't leave test objects behind, even on failure
	t.Cleanup(func() {
		if err := store.Delete(context.Background(), key); err != nil {
			t.Logf("cleanup delete %q: %v", key, err)
		}
	})

	// 1. direct Put -> Stat -> Get
	if err := store.Put(ctx, key, int64(len(payload)), bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	exists, size, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !exists {
		t.Fatal("Stat: object should exist after Put")
	}
	if size != int64(len(payload)) {
		t.Fatalf("Stat size = %d, want %d", size, len(payload))
	}

	if got := readObject(t, store, ctx, key); !bytes.Equal(got, payload) {
		t.Fatalf("Get returned %q, want %q", got, payload)
	}

	// 2. delete -> Stat reports gone
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, _, err = store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat after delete: %v", err)
	}
	if exists {
		t.Fatal("Stat: object should not exist after Delete")
	}

	// 3. presigned PUT (client -> bucket) then presigned GET (bucket -> client)
	const ttl = 5 * time.Minute
	presigned := []byte("presigned payload " + randomHex(t, 8))

	putURL, _, err := store.PresignPut(ctx, key, int64(len(presigned)), ttl)
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	httpDo(t, http.MethodPut, putURL, presigned)

	exists, size, err = store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat after presigned put: %v", err)
	}
	if !exists || size != int64(len(presigned)) {
		t.Fatalf("after presigned put: exists=%v size=%d, want exists=true size=%d", exists, size, len(presigned))
	}

	getURL, err := store.PresignGet(ctx, key, ttl)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if got := httpDo(t, http.MethodGet, getURL, nil); !bytes.Equal(got, presigned) {
		t.Fatalf("presigned get returned %q, want %q", got, presigned)
	}
}

func s3OptsFromEnv(t *testing.T) config.S3Config {
	t.Helper()

	bucket := os.Getenv("LFS_S3_BUCKET")
	endpoint := os.Getenv("LFS_S3_ENDPOINT")
	accessKey := os.Getenv("LFS_S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("LFS_S3_SECRET_ACCESS_KEY")
	if bucket == "" || endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("set LFS_S3_BUCKET, LFS_S3_ENDPOINT, LFS_S3_ACCESS_KEY_ID and LFS_S3_SECRET_ACCESS_KEY to run the S3 round-trip test")
	}

	return config.S3Config{
		Bucket:       bucket,
		Region:       os.Getenv("LFS_S3_REGION"),
		Endpoint:     endpoint,
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		UseSSL:       envBool("LFS_S3_USE_SSL", true),
		UsePathStyle: envBool("LFS_S3_USE_PATH_STYLE", false),
		KeyPrefix:    os.Getenv("LFS_S3_KEY_PREFIX"),
	}
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func readObject(t *testing.T, store *S3, ctx context.Context, key string) []byte {
	t.Helper()
	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	return data
}

// httpDo issues a raw HTTP request against a presigned URL, mimicking what a
// git-lfs client does, and returns the response body for GETs.
func httpDo(t *testing.T, method, url string, body []byte) []byte {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s presigned url: %v", method, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s presigned url: status %d, body: %s", method, resp.StatusCode, data)
	}
	return data
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}
