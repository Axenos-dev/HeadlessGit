package users

import (
	"errors"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type CreateUserRequest struct {
	Username string `json:"username"`
	Kind     string `json:"kind"`
}

func (r CreateUserRequest) Validate() error {
	if r.Username == "" {
		return errors.New("username is required")
	}
	if r.Kind != string(domain.UserKindUser) && r.Kind != string(domain.UserKindService) {
		return errors.New("kind must be 'user' or 'service'")
	}
	return nil
}

type UserResponse struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Kind     string `json:"kind"`
}

func newUserResponse(a domain.Account) UserResponse {
	return UserResponse{
		ID:       a.UserID,
		Username: a.Username,
		Kind:     string(a.Kind),
	}
}

type AddSSHKeyRequest struct {
	Title     string `json:"title"`
	PublicKey string `json:"publicKey"`
}

func (r AddSSHKeyRequest) Validate() error {
	if r.Title == "" {
		return errors.New("title is required")
	}
	if r.PublicKey == "" {
		return errors.New("publicKey is required")
	}
	return nil
}

type SSHKeyResponse struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Fingerprint string     `json:"fingerprint"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

func newSSHKeyResponse(k domain.SSHKey) SSHKeyResponse {
	return SSHKeyResponse{
		ID:          k.ID,
		Title:       k.Title,
		Fingerprint: k.Fingerprint,
		CreatedAt:   k.CreatedAt,
		LastUsedAt:  k.LastUsedAt,
	}
}

func newSSHKeyResponses(keys []domain.SSHKey) []SSHKeyResponse {
	out := make([]SSHKeyResponse, len(keys))
	for i, k := range keys {
		out[i] = newSSHKeyResponse(k)
	}
	return out
}

// TokenResponse never includes the raw token; that is only returned once at mint.
type TokenResponse struct {
	ID         int64      `json:"id"`
	Title      string     `json:"title"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

func newTokenResponse(t domain.Token) TokenResponse {
	return TokenResponse{
		ID:         t.ID,
		Title:      t.Title,
		CreatedAt:  t.CreatedAt,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
	}
}

func newTokenResponses(tokens []domain.Token) []TokenResponse {
	out := make([]TokenResponse, len(tokens))
	for i, t := range tokens {
		out[i] = newTokenResponse(t)
	}
	return out
}

type MintTokenRequest struct {
	Title string `json:"title"`
}

func (r MintTokenRequest) Validate() error {
	if r.Title == "" {
		return errors.New("title is required")
	}
	return nil
}

type MintTokenResponse struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Token string `json:"token"`
}

func newMintTokenResponse(raw string, t domain.Token) MintTokenResponse {
	return MintTokenResponse{
		ID:    t.ID,
		Title: t.Title,
		Token: raw,
	}
}
