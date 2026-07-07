package githttp_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp"
	"github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	"github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	"github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"go.uber.org/zap"
)

// The pre-receive shim execs os.Executable() — which, during tests, is this
// test binary rather than the server. Impersonate the server's hook mode so
// pushes exercised here run the real policy check instead of the test suite.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		os.Exit(gitbackend.HookMain(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// TestGitHTTPEndToEnd drives the real git binary against the full HTTP stack:
// DB resolution -> token auth -> authorization -> git pack backend.
func TestGitHTTPEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

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

	repoSvc := repositories.NewService(log, repositories.NewRegistry(database), backend, nil, nil)
	authSvc := auth.NewService(log, auth.NewRegistry(database))
	permsSvc := permissions.NewService(permissions.NewRegistry(database))
	usersSvc := users.NewService(users.NewRegistry(database))

	// seed: user "acme" owns a private repo, with a token to authenticate
	owner, err := usersSvc.Create(ctx, domain.UserInfo{Username: "acme", Kind: domain.UserKindUser})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := repoSvc.Create(ctx, owner.UserID, domain.RepositoryInfo{
		RepositoryName: "api", Visibility: domain.RepoVisibilityPrivate,
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	token, _, err := authSvc.MintToken(ctx, owner.UserID, "ci", nil)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// serve the git HTTP transport
	srv := githttp.NewServer(log, githttp.Services{
		Repositories:   repoSvc,
		Authentication: authSvc,
		Authorization:  permsSvc,
		Backend:        backend,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	anonURL := ts.URL + "/acme/api.git"
	authedURL := strings.Replace(ts.URL, "http://", "http://x:"+token+"@", 1) + "/acme/api.git"

	// 1. anonymous clone of a private repo must be rejected
	if err := runGit(t, "", "clone", anonURL, filepath.Join(t.TempDir(), "anon")); err == nil {
		t.Fatal("anonymous clone of a private repo should have failed")
	}

	// 2. token clone (empty), commit, push
	work := filepath.Join(t.TempDir(), "work")
	mustGit(t, "", "clone", authedURL, work)
	writeFile(t, filepath.Join(work, "f.txt"), "hello")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "add", ".")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	mustGit(t, work, "push", "origin", "HEAD")

	// 3. re-clone and confirm the pushed content round-tripped
	verify := filepath.Join(t.TempDir(), "verify")
	mustGit(t, "", "clone", authedURL, verify)
	if got := readFile(t, filepath.Join(verify, "f.txt")); got != "hello" {
		t.Fatalf("round-tripped content = %q, want %q", got, "hello")
	}

	// 4. path policy: a push touching a blocked path must be rejected by the
	// pre-receive hook, and the branch must not move
	repo, err := repoSvc.GetRepositoryByPath(ctx, "acme", "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoSvc.AddPathPolicy(ctx, repo.ID, "runtime", "deploy-managed state"); err != nil {
		t.Fatal(err)
	}

	// the branch name depends on the environment's init.defaultBranch
	// (main locally, master on bare CI runners) — never assume it
	branch := strings.TrimSpace(gitOut(t, work, "symbolic-ref", "--short", "HEAD"))
	headBefore := gitOut(t, work, "rev-parse", "origin/"+branch)

	if err := os.MkdirAll(filepath.Join(work, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, "runtime", "state.json"), "{}")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "add", ".")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "blocked")

	if err := runGit(t, work, "push", "origin", "HEAD"); err == nil {
		t.Fatal("push touching a blocked path should have been rejected")
	}
	mustGit(t, work, "fetch", "origin")
	if headAfter := gitOut(t, work, "rev-parse", "origin/"+branch); headAfter != headBefore {
		t.Fatalf("branch moved despite rejection: %s -> %s", headBefore, headAfter)
	}

	// 5. an unrelated push must still pass with the policy in place
	mustGit(t, work, "reset", "--hard", "HEAD~1")
	writeFile(t, filepath.Join(work, "ok.txt"), "fine")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "add", "ok.txt")
	mustGit(t, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "clean")
	mustGit(t, work, "push", "origin", "HEAD")
}

func runGit(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("git %v:\n%s", args, out)
	}
	return err
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := runGit(t, dir, args...); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
