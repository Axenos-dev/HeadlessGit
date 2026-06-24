package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

// openTestDB opens a fresh, migrated SQLite in a temp dir. Reaching here at all
// proves the migrations apply cleanly.
func openTestDB(t *testing.T) *DB {
	t.Helper()

	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := d.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestResolveRepoByPath(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	user, err := d.CreateUser(ctx, gen.CreateUserParams{Username: "acme", Kind: "user"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := d.CreateRepository(ctx, gen.CreateRepositoryParams{
		OwnerID: user.ID, RepositoryName: "api", StoragePath: "1/api.git", Visibility: "private",
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// the join: username + repo name -> the repo
	repo, err := d.GetRepositoryByPath(ctx, gen.GetRepositoryByPathParams{Namespace: "acme", Name: "api"})
	if err != nil {
		t.Fatalf("get by path: %v", err)
	}
	if repo.OwnerID != user.ID || repo.StoragePath != "1/api.git" {
		t.Fatalf("unexpected repo: %+v", repo)
	}

	if _, err := d.GetRepositoryByPath(ctx, gen.GetRepositoryByPathParams{Namespace: "acme", Name: "nope"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing repo: expected ErrNoRows, got %v", err)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// owner 999 doesn't exist; only fails if the foreign_keys pragma is on
	_, err := d.CreateRepository(ctx, gen.CreateRepositoryParams{
		OwnerID: 999, RepositoryName: "x", StoragePath: "x.git", Visibility: "private",
	})
	if err == nil {
		t.Fatal("expected foreign key violation, got nil (pragma not applied?)")
	}
}

func TestCredentialLookups(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	user, _ := d.CreateUser(ctx, gen.CreateUserParams{Username: "bob", Kind: "user"})

	if _, err := d.CreateSSHKey(ctx, gen.CreateSSHKeyParams{
		UserID: user.ID, Title: "k", PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:abc",
	}); err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	if got, err := d.GetUserByFingerprint(ctx, "SHA256:abc"); err != nil || got.ID != user.ID {
		t.Fatalf("by fingerprint: got %+v, err %v", got, err)
	}

	if _, err := d.CreateToken(ctx, gen.CreateTokenParams{UserID: user.ID, Title: "t", TokenHash: "hash1"}); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if got, err := d.GetUserByToken(ctx, "hash1"); err != nil || got.ID != user.ID {
		t.Fatalf("by token: got %+v, err %v", got, err)
	}
}

func TestExpiredTokenNotResolved(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	user, _ := d.CreateUser(ctx, gen.CreateUserParams{Username: "carol", Kind: "user"})

	past := time.Now().Add(-time.Hour).UnixMilli()
	if _, err := d.CreateToken(ctx, gen.CreateTokenParams{
		UserID: user.ID, Title: "t", TokenHash: "expired",
		ExpiresAtUnixMs: sql.NullInt64{Int64: past, Valid: true},
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}

	// the expiry filter in the query should hide it
	if _, err := d.GetUserByToken(ctx, "expired"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired token should not resolve, got %v", err)
	}
}

func TestPermissionUpsert(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	owner, _ := d.CreateUser(ctx, gen.CreateUserParams{Username: "o", Kind: "user"})
	collab, _ := d.CreateUser(ctx, gen.CreateUserParams{Username: "c", Kind: "user"})
	repo, _ := d.CreateRepository(ctx, gen.CreateRepositoryParams{
		OwnerID: owner.ID, RepositoryName: "r", StoragePath: "o/r.git", Visibility: "private",
	})

	if _, err := d.UpsertPermission(ctx, gen.UpsertPermissionParams{UserID: collab.ID, RepositoryID: repo.ID, UserRole: "read"}); err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	// conflict on (user, repo) -> update the role
	if _, err := d.UpsertPermission(ctx, gen.UpsertPermissionParams{UserID: collab.ID, RepositoryID: repo.ID, UserRole: "write"}); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	p, err := d.GetPermission(ctx, gen.GetPermissionParams{UserID: collab.ID, RepositoryID: repo.ID})
	if err != nil {
		t.Fatalf("get permission: %v", err)
	}
	if p.UserRole != "write" {
		t.Fatalf("role = %q, want write", p.UserRole)
	}
}

func TestEnsureAdminUserIdempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a1, err := d.EnsureAdminUser(ctx)
	if err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	a2, err := d.EnsureAdminUser(ctx)
	if err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if a1.ID != a2.ID {
		t.Fatalf("admin id changed across calls: %d -> %d", a1.ID, a2.ID)
	}
	if a2.IsAdmin == 0 {
		t.Fatal("admin user not marked is_admin")
	}
}
