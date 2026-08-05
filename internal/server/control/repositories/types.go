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
	Name       string         `json:"name"`
	Path       string         `json:"path"`
	Type       string         `json:"type"` // file | dir | symlink | submodule
	Mode       string         `json:"mode"`
	Size       *int64         `json:"size,omitempty"` // blobs only; note: LFS pointers report pointer size
	SHA        string         `json:"sha"`
	LastCommit *CommitSummary `json:"lastCommit,omitempty"`
}

type CommitSummary struct {
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	CommittedAt time.Time `json:"committedAt"`
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
	if e.LastCommit != nil {
		entry.LastCommit = &CommitSummary{
			SHA:         e.LastCommit.SHA,
			Message:     e.LastCommit.Message,
			CommittedAt: e.LastCommit.CommittedAt,
		}
	}
	if e.Size >= 0 {
		size := e.Size
		entry.Size = &size
	}
	return entry
}

type Diff struct {
	BaseSHA   string     `json:"baseSha"`
	HeadSHA   string     `json:"headSha"`
	Files     []DiffFile `json:"files"`
	Truncated bool       `json:"truncated"`
}

type DiffFile struct {
	Status             domain.DiffStatus             `json:"status"`
	OldPath            string                        `json:"oldPath,omitempty"`
	NewPath            string                        `json:"newPath,omitempty"`
	OldBlobSHA         string                        `json:"oldBlobSha,omitempty"`
	NewBlobSHA         string                        `json:"newBlobSha,omitempty"`
	OldMode            string                        `json:"oldMode,omitempty"`
	NewMode            string                        `json:"newMode,omitempty"`
	Additions          *int64                        `json:"additions"`
	Deletions          *int64                        `json:"deletions"`
	Binary             bool                          `json:"binary"`
	Patch              *string                       `json:"patch"`
	PatchOmittedReason domain.DiffPatchOmittedReason `json:"patchOmittedReason,omitempty"`
}

func newDiff(diff domain.RepositoryDiff) Diff {
	files := make([]DiffFile, len(diff.Files))
	for i, file := range diff.Files {
		files[i] = DiffFile{
			Status:             file.Status,
			OldPath:            file.OldPath,
			NewPath:            file.NewPath,
			OldBlobSHA:         file.OldBlobSHA,
			NewBlobSHA:         file.NewBlobSHA,
			OldMode:            file.OldMode,
			NewMode:            file.NewMode,
			Binary:             file.Binary,
			Patch:              file.Patch,
			PatchOmittedReason: file.PatchOmittedReason,
		}
		if !file.Binary {
			additions, deletions := file.Additions, file.Deletions
			files[i].Additions = &additions
			files[i].Deletions = &deletions
		}
	}
	return Diff{
		BaseSHA:   diff.BaseSHA,
		HeadSHA:   diff.HeadSHA,
		Files:     files,
		Truncated: diff.Truncated,
	}
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

type CommitDetails struct {
	SHA         string       `json:"sha"`
	Parents     []string     `json:"parents"`
	Message     string       `json:"message"`
	Author      CommitAuthor `json:"author"`
	AuthoredAt  time.Time    `json:"authoredAt"`
	CommittedAt time.Time    `json:"committedAt"`
}

func newCommitDetails(commit domain.CommitDetails) CommitDetails {
	return CommitDetails{
		SHA:     commit.SHA,
		Parents: commit.Parents,
		Message: commit.Message,
		Author: CommitAuthor{
			Name:  commit.Author.Name,
			Email: commit.Author.Email,
		},
		AuthoredAt:  commit.AuthoredAt,
		CommittedAt: commit.CommittedAt,
	}
}

type CommitObjectLfs struct {
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

type CommitOperation struct {
	Op         string           `json:"op"` // "put" | "delete"
	Path       string           `json:"path"`
	Lfs        *CommitObjectLfs `json:"lfs,omitempty"`        // puts only, from POST .../lfs/objects/batch
	BlobSHA    *string          `json:"blobSha,omitempty"`    // puts only, from POST /blobs
	Executable bool             `json:"executable,omitempty"` // puts only
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
			if (op.BlobSHA == nil) == (op.Lfs == nil) {
				return fmt.Errorf("operations[%d]: exactly one of blobSha or lfs is required for put", i)
			}
			if op.BlobSHA != nil {
				if *op.BlobSHA == "" {
					return fmt.Errorf("operations[%d]: blobSha is required for put", i)
				}
			} else {
				if op.Lfs.OID == "" {
					return fmt.Errorf("operations[%d]: lfs.oid is required", i)
				}
				if op.Lfs.Size <= 0 {
					return fmt.Errorf("operations[%d]: lfs.size must be positive", i)
				}
			}
		case "delete":
			if op.BlobSHA != nil || op.Lfs != nil || op.Executable {
				return fmt.Errorf("operations[%d]: delete takes no blobSha, lfs, or executable", i)
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

type AddPathPolicyRequest struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason,omitempty"`
}

func (r AddPathPolicyRequest) Validate() error {
	if r.Pattern == "" {
		return errors.New("pattern is required")
	}
	return nil
}

type PathPolicy struct {
	ID        int64     `json:"id"`
	Pattern   string    `json:"pattern"`
	Kind      string    `json:"kind"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func newPathPolicy(p domain.PathPolicy) PathPolicy {
	return PathPolicy{
		ID:        p.ID,
		Pattern:   p.Pattern,
		Kind:      string(p.Kind),
		Reason:    p.Reason,
		CreatedAt: p.CreatedAt,
	}
}
