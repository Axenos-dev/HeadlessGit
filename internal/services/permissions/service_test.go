package permissions

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

// fakeRegistry returns a single configured grant for any (user, repo), or
// sql.ErrNoRows when hasGrant is false.
type fakeRegistry struct {
	grant    string
	hasGrant bool
}

func (f fakeRegistry) GetPermission(ctx context.Context, userID, repositoryID int64) (gen.Permission, error) {
	if !f.hasGrant {
		return gen.Permission{}, sql.ErrNoRows
	}
	return gen.Permission{UserRole: f.grant}, nil
}

func (f fakeRegistry) UpsertPermission(ctx context.Context, userID, repositoryID int64, role string) (gen.Permission, error) {
	return gen.Permission{}, nil
}

func (f fakeRegistry) DeletePermission(ctx context.Context, userID, repositoryID int64) error {
	return nil
}

func TestAuthorize(t *testing.T) {
	owner := &domain.Account{UserID: 1}
	other := &domain.Account{UserID: 2}
	admin := &domain.Account{UserID: 3, IsAdmin: true}

	priv := domain.Repository{ID: 10, OwnerID: 1, Visibility: domain.RepoVisibilityPrivate}
	pub := domain.Repository{ID: 11, OwnerID: 1, Visibility: domain.RepoVisibilityPublic}

	cases := []struct {
		name     string
		account  *domain.Account
		repo     domain.Repository
		grant    string // explicit grant role for non-owner; "" = none
		required domain.Role
		allow    bool
	}{
		{"anonymous private read", nil, priv, "", domain.RoleRead, false},
		{"anonymous public read", nil, pub, "", domain.RoleRead, true},
		{"anonymous public write", nil, pub, "", domain.RoleWrite, false},

		{"owner private write", owner, priv, "", domain.RoleWrite, true},
		{"owner private admin", owner, priv, "", domain.RoleAdmin, true},

		{"other private no grant", other, priv, "", domain.RoleRead, false},
		{"other private read grant, read", other, priv, "read", domain.RoleRead, true},
		{"other private read grant, write", other, priv, "read", domain.RoleWrite, false},
		{"other private write grant, write", other, priv, "write", domain.RoleWrite, true},

		{"admin private admin", admin, priv, "", domain.RoleAdmin, true},
		{"admin private write", admin, priv, "", domain.RoleWrite, true},

		{"other public no grant write", other, pub, "", domain.RoleWrite, false},
		{"other public write grant, write", other, pub, "write", domain.RoleWrite, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(fakeRegistry{grant: tc.grant, hasGrant: tc.grant != ""})
			err := svc.Authorize(context.Background(), tc.account, tc.repo, tc.required)

			switch {
			case tc.allow && err != nil:
				t.Fatalf("expected allow, got %v", err)
			case !tc.allow && !errors.Is(err, ErrAccessDenied):
				t.Fatalf("expected ErrAccessDenied, got %v", err)
			}
		})
	}
}
