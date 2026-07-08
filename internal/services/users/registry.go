package users

import (
	"context"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

type UserRegistry struct {
	db *db.DB
}

func NewRegistry(db *db.DB) *UserRegistry {
	return &UserRegistry{
		db: db,
	}
}

func (r *UserRegistry) GetUser(ctx context.Context, userID int64) (gen.User, error) {
	return r.db.GetUser(ctx, userID)
}

func (r *UserRegistry) GetUserByUsername(ctx context.Context, username string) (gen.User, error) {
	return r.db.GetUserByUsername(ctx, username)
}

func (r *UserRegistry) CreateUser(ctx context.Context, username, kind string) (gen.User, error) {
	return r.db.CreateUser(ctx, gen.CreateUserParams{
		Username: username,
		Kind:     kind,
	})
}
