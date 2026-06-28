package githttp_test

import (
	"context"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/config"
	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp"
	"github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	"github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	"github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"github.com/Axenos-dev/HeadlessGit/internal/storage"
	"go.uber.org/zap"
)

// TestGitLFSEndToEnd drives a real git + git-lfs client against the full HTTP
// stack backed by an S3/R2 bucket. It proves the whole chain: clean filter ->
// batch API -> presigned upload to the bucket -> verify, then a fresh clone
// pulling the object back down via a presigned download.
//
// Skipped unless git-lfs is installed AND the LFS_S3_* env vars are set:
//
//	LFS_S3_BUCKET=my-bucket LFS_S3_REGION=auto \
//	LFS_S3_ENDPOINT=<account>.r2.cloudflarestorage.com \
//	LFS_S3_ACCESS_KEY_ID=... LFS_S3_SECRET_ACCESS_KEY=... \
//	go test ./internal/server/git/githttp/ -run TestGitLFSEndToEnd -v
func TestGitLFSEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs not installed")
	}
	s3cfg := s3ConfigFromEnv(t)

	ctx := context.Background()
	log := zap.NewNop()
	repoRoot := t.TempDir()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	backend, err := gitbackend.NewLocal(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	repoSvc := repositories.NewService(log, repositories.NewRegistry(database), backend)
	authSvc := auth.NewService(log, auth.NewRegistry(database))
	permsSvc := permissions.NewService(permissions.NewRegistry(database))
	usersSvc := users.NewService(users.NewRegistry(database))

	owner, err := usersSvc.Create(ctx, domain.UserInfo{Username: "acme", Kind: domain.UserKindUser})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := repoSvc.Create(ctx, owner.UserID, domain.RepositoryInfo{
		RepositoryName: "lfs", Visibility: domain.RepoVisibilityPrivate,
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	token, _, err := authSvc.MintToken(ctx, owner.UserID, "ci", nil)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// the LFS service needs the server's own public URL (for the verify action),
	// so create the listener first to learn the address, then wire it up.
	ts := httptest.NewUnstartedServer(nil)
	t.Cleanup(ts.Close)
	publicURL := "http://" + ts.Listener.Addr().String()

	store, err := storage.NewS3(s3cfg)
	if err != nil {
		t.Fatalf("init s3: %v", err)
	}
	lfsSvc := lfs.NewService(log, lfs.NewRegistry(database), store, publicURL)

	srv := githttp.NewServer(log, githttp.Services{
		Repositories:   repoSvc,
		Authentication: authSvc,
		Authorization:  permsSvc,
		Backend:        backend,
		LFS:            lfsSvc,
	})
	ts.Config.Handler = srv.Handler()
	ts.Start()

	authedURL := strings.Replace(publicURL, "http://", "http://x:"+token+"@", 1) + "/acme/lfs.git"

	// a real binary blob that git-lfs will offload to the bucket
	content := randomBytes(t, 1<<20) // 1 MiB

	// 1. clone empty, set up lfs tracking, commit a tracked binary, push
	work := filepath.Join(t.TempDir(), "work")
	mustGit(t, "", "clone", authedURL, work)
	mustGit(t, work, "lfs", "install", "--local")
	mustGit(t, work, "config", "--local", "lfs.locksverify", "false")
	mustGit(t, work, "lfs", "track", "*.bin")
	writeFile(t, filepath.Join(work, "data.bin"), string(content))
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "add", ".")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "add lfs object")
	mustGit(t, work, "push", "origin", "HEAD")

	// 2. the committed blob must be an LFS pointer, not the raw bytes -> proves
	//    the object went through LFS (and thus to the bucket), not into git
	ptr := gitOut(t, work, "show", "HEAD:data.bin")
	if !strings.HasPrefix(ptr, "version https://git-lfs") {
		t.Fatalf("expected an LFS pointer for data.bin, got:\n%.120s", ptr)
	}

	// 3. fresh clone + pull the object back down from the bucket
	verify := filepath.Join(t.TempDir(), "verify")
	mustGit(t, "", "clone", authedURL, verify)
	mustGit(t, verify, "lfs", "install", "--local")
	mustGit(t, verify, "lfs", "pull")

	if got := readFile(t, filepath.Join(verify, "data.bin")); got != string(content) {
		t.Fatalf("round-tripped LFS object differs: got %d bytes, want %d", len(got), len(content))
	}
}

func s3ConfigFromEnv(t *testing.T) config.S3Config {
	t.Helper()

	bucket := os.Getenv("LFS_S3_BUCKET")
	endpoint := os.Getenv("LFS_S3_ENDPOINT")
	accessKey := os.Getenv("LFS_S3_ACCESS_KEY_ID")
	secretKey := os.Getenv("LFS_S3_SECRET_ACCESS_KEY")
	if bucket == "" || endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("set LFS_S3_BUCKET, LFS_S3_ENDPOINT, LFS_S3_ACCESS_KEY_ID and LFS_S3_SECRET_ACCESS_KEY to run the LFS e2e test")
	}

	parseBool := func(key string, def bool) bool {
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

	return config.S3Config{
		Bucket:       bucket,
		Region:       os.Getenv("LFS_S3_REGION"),
		Endpoint:     endpoint,
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		UseSSL:       parseBool("LFS_S3_USE_SSL", true),
		UsePathStyle: parseBool("LFS_S3_USE_PATH_STYLE", false),
		KeyPrefix:    os.Getenv("LFS_S3_KEY_PREFIX"),
	}
}

// gitOut runs a git command and returns its combined output, failing the test
// on a non-zero exit.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}
