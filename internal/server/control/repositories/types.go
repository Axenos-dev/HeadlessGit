package repositories

import (
	"errors"
	"fmt"
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

func newRepositories(repos []domain.Repository) []Repository {
	out := make([]Repository, len(repos))
	for i, repo := range repos {
		out[i] = newRepository(repo)
	}
	return out
}

type Contents struct {
	Ref       string         `json:"ref"`
	SHA       string         `json:"sha"`
	Path      string         `json:"path"`
	Entries   []ContentEntry `json:"entries"`
	Truncated bool           `json:"truncated,omitempty"`
}

type ContentEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // file | dir | symlink | submodule
	Mode string `json:"mode"`
	Size *int64 `json:"size,omitempty"` // blobs only; note: LFS pointers report pointer size
	SHA  string `json:"sha"`
}

func newContents(c domain.RepositoryContents) Contents {
	entries := make([]ContentEntry, len(c.Entries))
	for i, e := range c.Entries {
		entries[i] = newContentEntry(e)
	}
	return Contents{
		Ref:       c.Ref,
		SHA:       c.CommitSHA,
		Path:      c.Path,
		Entries:   entries,
		Truncated: c.Truncated,
	}
}

func newContentEntry(e domain.TreeEntry) ContentEntry {
	entry := ContentEntry{
		Name: e.Name,
		Path: e.Path,
		Type: string(e.Type),
		Mode: e.Mode,
		SHA:  e.SHA,
	}
	if e.Size >= 0 {
		size := e.Size
		entry.Size = &size
	}
	return entry
}

type UpdateVisibilityRequest struct {
	Visibility string `json:"visibility"`
}

func (r UpdateVisibilityRequest) Validate() error {
	if r.Visibility != string(domain.RepoVisibilityPublic) && r.Visibility != string(domain.RepoVisibilityPrivate) {
		return errors.New("visibility must be 'public' or 'private'")
	}
	return nil
}

type UploadBlobResponse struct {
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type CommitAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CommitOperation struct {
	Op         string `json:"op"` // "put" | "delete"
	Path       string `json:"path"`
	BlobSHA    string `json:"blobSha,omitempty"`    // puts only, from POST /blobs
	Executable bool   `json:"executable,omitempty"` // puts only
}

type CreateCommitRequest struct {
	Branch          string            `json:"branch"`
	Message         string            `json:"message"`
	Author          CommitAuthor      `json:"author"`
	ExpectedHeadSHA string            `json:"expectedHeadSha,omitempty"`
	PusherID        int64             `json:"pusherId,omitempty"`
	Operations      []CommitOperation `json:"operations"`
}

func (r CreateCommitRequest) Validate() error {
	if r.Branch == "" {
		return errors.New("branch is required")
	}
	if r.Message == "" {
		return errors.New("message is required")
	}
	if r.Author.Name == "" || r.Author.Email == "" {
		return errors.New("author name and email are required")
	}
	if len(r.Operations) == 0 {
		return errors.New("operations must not be empty")
	}
	for i, op := range r.Operations {
		if op.Path == "" {
			return fmt.Errorf("operations[%d]: path is required", i)
		}
		switch op.Op {
		case "put":
			if op.BlobSHA == "" {
				return fmt.Errorf("operations[%d]: blobSha is required for put", i)
			}
		case "delete":
			if op.BlobSHA != "" || op.Executable {
				return fmt.Errorf("operations[%d]: delete takes no blobSha or executable", i)
			}
		default:
			return fmt.Errorf("operations[%d]: op must be 'put' or 'delete'", i)
		}
	}
	return nil
}

type Commit struct {
	Branch    string `json:"branch"`
	CommitSHA string `json:"commitSha"`
	Before    string `json:"before"` // the head the commit was built on
}

func newCommit(res domain.CommitResult) Commit {
	return Commit{
		Branch:    res.Branch,
		CommitSHA: res.CommitSHA,
		Before:    res.Before,
	}
}
