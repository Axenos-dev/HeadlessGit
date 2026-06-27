package auth

import (
	"context"
	"database/sql"

	"github.com/Axenos-dev/HeadlessGit/internal/db"
	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
)

type AuthRegistry struct {
	db *db.DB
}

func NewRegistry(db *db.DB) *AuthRegistry {
	return &AuthRegistry{
		db: db,
	}
}

// ssh keys

func (r *AuthRegistry) GetUserByFingerprint(ctx context.Context, fingerprint string) (gen.User, error) {
	return r.db.GetUserByFingerprint(ctx, fingerprint)
}

func (r *AuthRegistry) CreateSSHKey(ctx context.Context, userID int64, title, publicKey, fingerprint string) (gen.SshKey, error) {
	return r.db.CreateSSHKey(ctx, gen.CreateSSHKeyParams{
		UserID:      userID,
		Title:       title,
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
	})
}

func (r *AuthRegistry) DeleteSSHKey(ctx context.Context, fingerprint string) error {
	return r.db.DeleteSSHKey(ctx, fingerprint)
}

func (r *AuthRegistry) DeleteSSHKeyByID(ctx context.Context, keyID, userID int64) (int64, error) {
	return r.db.DeleteSSHKeyByID(ctx, gen.DeleteSSHKeyByIDParams{ID: keyID, UserID: userID})
}

func (r *AuthRegistry) ListSSHKeysByUser(ctx context.Context, userID int64) ([]gen.SshKey, error) {
	return r.db.ListSSHKeysByUser(ctx, userID)
}

func (r *AuthRegistry) UpdateSSHKeyUsedAt(ctx context.Context, fingerprint string) error {
	return r.db.UpdateSSHKeyUsedAt(ctx, fingerprint)
}

// tokens

func (r *AuthRegistry) GetUserByToken(ctx context.Context, tokenHash string) (gen.User, error) {
	return r.db.GetUserByToken(ctx, tokenHash)
}

func (r *AuthRegistry) CreateToken(ctx context.Context, userID int64, title, tokenHash string, expiresAtUnixMs *int64) (gen.Token, error) {
	return r.db.CreateToken(ctx, gen.CreateTokenParams{
		UserID:          userID,
		Title:           title,
		TokenHash:       tokenHash,
		ExpiresAtUnixMs: nullInt64(expiresAtUnixMs),
	})
}

func (r *AuthRegistry) DeleteToken(ctx context.Context, tokenHash string) error {
	return r.db.DeleteToken(ctx, tokenHash)
}

func (r *AuthRegistry) DeleteTokenByID(ctx context.Context, tokenID, userID int64) (int64, error) {
	return r.db.DeleteTokenByID(ctx, gen.DeleteTokenByIDParams{ID: tokenID, UserID: userID})
}

func (r *AuthRegistry) ListTokensByUser(ctx context.Context, userID int64) ([]gen.Token, error) {
	return r.db.ListTokensByUser(ctx, userID)
}

func (r *AuthRegistry) DeleteTokensByUserID(ctx context.Context, userID int64) error {
	return r.db.DeleteTokensByUserID(ctx, userID)
}

func (r *AuthRegistry) DeleteExpiredTokens(ctx context.Context) (int64, error) {
	return r.db.DeleteExpiredTokens(ctx)
}

func (r *AuthRegistry) UpdateTokenUsedAt(ctx context.Context, tokenHash string) error {
	return r.db.UpdateTokenUsedAt(ctx, tokenHash)
}

func (r *AuthRegistry) EnsureAdminUser(ctx context.Context) (gen.User, error) {
	return r.db.EnsureAdminUser(ctx)
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
