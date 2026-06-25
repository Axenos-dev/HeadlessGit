package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type Registry interface {
	GetUserByFingerprint(ctx context.Context, fingerprint string) (gen.User, error)
	CreateSSHKey(ctx context.Context, userID int64, title, publicKey, fingerprint string) (gen.SshKey, error)
	DeleteSSHKey(ctx context.Context, fingerprint string) error
	UpdateSSHKeyUsedAt(ctx context.Context, fingerprint string) error

	GetUserByToken(ctx context.Context, tokenHash string) (gen.User, error)
	CreateToken(ctx context.Context, userID int64, title, tokenHash string, expiresAtUnixMs *int64) (gen.Token, error)
	DeleteToken(ctx context.Context, tokenHash string) error
	DeleteTokensByUserID(ctx context.Context, userID int64) error
	DeleteExpiredTokens(ctx context.Context) (int64, error)
	UpdateTokenUsedAt(ctx context.Context, tokenHash string) error

	EnsureAdminUser(ctx context.Context) (gen.User, error)
}

type Service struct {
	logger   *zap.Logger
	registry Registry
}

func NewService(logger *zap.Logger, registry Registry) *Service {
	return &Service{
		logger:   logger,
		registry: registry,
	}
}

// ensures the admin service account exists
// and that its only token is the given one
// rotating token and restarting -> replaces the old token
func (s *Service) SeedAdmin(ctx context.Context, rawToken string) error {
	admin, err := s.registry.EnsureAdminUser(ctx)
	if err != nil {
		return err
	}

	// delete all admin tokens
	if err := s.registry.DeleteTokensByUserID(ctx, admin.ID); err != nil {
		return err
	}

	// and create only one token for admin
	_, err = s.registry.CreateToken(ctx, admin.ID, "admin", hashToken(rawToken), nil)
	return err
}

// authentication

func (s *Service) AuthenticateSSHKey(ctx context.Context, fingerprint string) (domain.Account, error) {
	user, err := s.registry.GetUserByFingerprint(ctx, fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, ErrInvalidCredentials
	}
	if err != nil {
		return domain.Account{}, err
	}

	s.touch("ssh key", func() error { return s.registry.UpdateSSHKeyUsedAt(ctx, fingerprint) })
	return toAccount(user), nil
}

func (s *Service) AuthenticateToken(ctx context.Context, rawToken string) (domain.Account, error) {
	hash := hashToken(rawToken)

	// the query also filters out expired tokens
	user, err := s.registry.GetUserByToken(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, ErrInvalidCredentials
	}
	if err != nil {
		return domain.Account{}, err
	}

	s.touch("token", func() error { return s.registry.UpdateTokenUsedAt(ctx, hash) })
	return toAccount(user), nil
}

// credential management

func (s *Service) AddSSHKey(ctx context.Context, userID int64, title, publicKey string) (domain.SSHKey, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return domain.SSHKey{}, ErrInvalidSSHKey
	}

	fingerprint := ssh.FingerprintSHA256(pub)
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))

	key, err := s.registry.CreateSSHKey(ctx, userID, title, normalized, fingerprint)
	if err != nil {
		return domain.SSHKey{}, err
	}
	return sshKeyToDomain(key), nil
}

func (s *Service) RemoveSSHKey(ctx context.Context, fingerprint string) error {
	return s.registry.DeleteSSHKey(ctx, fingerprint)
}

// MintToken creates a token and returns the raw secret
// only its hash is stored
func (s *Service) MintToken(ctx context.Context, userID int64, title string, expiresAt *time.Time) (string, domain.Token, error) {
	raw, hash, err := generateToken()
	if err != nil {
		return "", domain.Token{}, err
	}

	var expiresAtUnixMs *int64
	if expiresAt != nil {
		ms := expiresAt.UnixMilli()
		expiresAtUnixMs = &ms
	}

	token, err := s.registry.CreateToken(ctx, userID, title, hash, expiresAtUnixMs)
	if err != nil {
		return "", domain.Token{}, err
	}
	return raw, tokenToDomain(token), nil
}

// token maintenance

// periodically deletes expired tokens until ctx is cancelled
func (s *Service) RunExpiredTokenGC(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.registry.DeleteExpiredTokens(ctx)
			if err != nil {
				s.logger.Warn("expired token gc failed", zap.Error(err))
				continue
			}
			if n > 0 {
				s.logger.Info("deleted expired tokens", zap.Int64("count", n))
			}
		}
	}
}

// helpers

// touch bumps a credential's last-used timestamp (best-effort)
func (s *Service) touch(kind string, fn func() error) {
	if err := fn(); err != nil {
		s.logger.Warn("failed to update credential last-used", zap.String("kind", kind), zap.Error(err))
	}
}

func generateToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw), nil
}

// hashToken is how every token (including the seeded admin token) is hashed
// before storage, so they all authenticate through the same lookup.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func toAccount(u gen.User) domain.Account {
	return domain.Account{
		UserID:   u.ID,
		Username: u.Username,
		Kind:     domain.UserKind(u.Kind),
		IsAdmin:  u.IsAdmin != 0,
	}
}

func sshKeyToDomain(k gen.SshKey) domain.SSHKey {
	key := domain.SSHKey{
		ID:          k.ID,
		Title:       k.Title,
		Fingerprint: k.Fingerprint,
		CreatedAt:   time.UnixMilli(k.CreatedAtUnixMs).UTC(),
	}
	if k.LastUsedAtUnixMs.Valid {
		t := time.UnixMilli(k.LastUsedAtUnixMs.Int64).UTC()
		key.LastUsedAt = &t
	}
	return key
}

func tokenToDomain(t gen.Token) domain.Token {
	token := domain.Token{
		ID:        t.ID,
		Title:     t.Title,
		CreatedAt: time.UnixMilli(t.CreatedAtUnixMs).UTC(),
	}
	if t.ExpiresAtUnixMs.Valid {
		v := time.UnixMilli(t.ExpiresAtUnixMs.Int64).UTC()
		token.ExpiresAt = &v
	}
	if t.LastUsedAtUnixMs.Valid {
		v := time.UnixMilli(t.LastUsedAtUnixMs.Int64).UTC()
		token.LastUsedAt = &v
	}
	return token
}
