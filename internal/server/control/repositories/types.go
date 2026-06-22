package repositories

import (
	"errors"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type CreateRepositoryRequest struct {
	OwnerID    int64  `json:"ownerId"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

func (r CreateRepositoryRequest) Validate() error {
	if r.OwnerID == 0 {
		return errors.New("ownerId is required")
	}
	if r.Name == "" {
		return errors.New("name is required")
	}
	if r.Visibility != string(domain.RepoVisibilityPublic) && r.Visibility != string(domain.RepoVisibilityPrivate) {
		return errors.New("visibility must be 'public' or 'private'")
	}
	return nil
}

type Repository struct {
	ID         int64      `json:"id"`
	OwnerID    int64      `json:"ownerId"`
	Name       string     `json:"name"`
	Visibility string     `json:"visibility"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

func newRepository(r domain.Repository) Repository {
	return Repository{
		ID:         r.ID,
		OwnerID:    r.OwnerID,
		Name:       r.RepositoryName,
		Visibility: string(r.Visibility),
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}
