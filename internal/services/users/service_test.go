package users

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type fakeRegistry struct {
	user gen.User
	err  error
}

func (f fakeRegistry) GetUser(ctx context.Context, userID int64) (gen.User, error) {
	return f.user, f.err
}

func (f fakeRegistry) CreateUser(ctx context.Context, username, kind string) (gen.User, error) {
	return f.user, f.err
}

func TestCreate(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		svc := NewService(fakeRegistry{user: gen.User{ID: 7, Username: "alice", Kind: "user"}})
		account, err := svc.Create(context.Background(), domain.UserInfo{Username: "alice", Kind: domain.UserKindUser})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if account.UserID != 7 || account.Username != "alice" {
			t.Errorf("unexpected account: %+v", account)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		// the insert is "on conflict do nothing returning *" -> on duplicate "no rows"
		svc := NewService(fakeRegistry{err: sql.ErrNoRows})
		if _, err := svc.Create(context.Background(), domain.UserInfo{Username: "alice", Kind: domain.UserKindUser}); !errors.Is(err, ErrUserExists) {
			t.Errorf("want ErrUserExists, got %v", err)
		}
	})

	t.Run("registry error", func(t *testing.T) {
		boom := errors.New("boom")
		svc := NewService(fakeRegistry{err: boom})
		if _, err := svc.Create(context.Background(), domain.UserInfo{Username: "alice", Kind: domain.UserKindUser}); !errors.Is(err, boom) {
			t.Errorf("want boom, got %v", err)
		}
	})
}
