package repositories

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"go.uber.org/zap"
)

const testSHA = "aaaabbbbccccddddeeeeffff0000111122223333"

type fakeRegistry struct {
	Registry
	repo gen.Repository
	err  error

	createRepoErr error

	policies        []gen.PathPolicy
	policiesErr     error
	createPolicyErr error
}

func (f fakeRegistry) GetRepository(ctx context.Context, repositoryID int64) (gen.Repository, error) {
	return f.repo, f.err
}

func (f fakeRegistry) CreateRepository(ctx context.Context, ownerID int64, name, storagePath, visibility string) (gen.Repository, error) {
	if f.createRepoErr != nil {
		return gen.Repository{}, f.createRepoErr
	}
	return f.repo, nil
}

func (f fakeRegistry) ListRepositoryPathPolicies(ctx context.Context, repositoryID int64) ([]gen.PathPolicy, error) {
	return f.policies, f.policiesErr
}

func (f fakeRegistry) CreateRepositoryPathPolicy(ctx context.Context, repositoryID int64, kind, pattern string, reason *string) (gen.PathPolicy, error) {
	if f.createPolicyErr != nil {
		return gen.PathPolicy{}, f.createPolicyErr
	}
	p := gen.PathPolicy{ID: 1, RepositoryID: repositoryID, Kind: kind, Pattern: pattern}
	if reason != nil {
		p.Reason = sql.NullString{String: *reason, Valid: true}
	}
	return p, nil
}

func (f fakeRegistry) DeleteRepositoryPathPolicy(ctx context.Context, repositoryID, pathPolicyID int64) error {
	return nil
}

type fakeStorage struct {
	RepositoryStorage
	sha        string
	resolveErr error
	tarBytes   []byte

	listing gitbackend.TreeListing
	listErr error
	listFn  func(storagePath, rev, treePath string, opts gitbackend.ListTreeOptions)

	diffResult gitbackend.DiffResult
	diffErr    error

	commitDetails gitbackend.CommitDetails
	commitErr     error
	commitFn      func(storagePath, sha string)

	blobInfo    gitbackend.BlobInfo
	blobStatErr error
	blobContent string

	writeBlobSHA string
	applyChange  gitbackend.RefChange
	applyErr     error
	// optional hook to inspect (and exercise) what ApplyCommit received
	applyFn        func(spec gitbackend.CommitSpec, ops []gitbackend.CommitOp, clean gitbackend.CleanFunc) error
	checkWritePath []string
}

func (f fakeStorage) InitBare(ctx context.Context, storagePath string) error {
	return nil
}

func (f fakeStorage) ResolveCommit(ctx context.Context, storagePath, rev string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.sha, nil
}

func (f fakeStorage) ListTree(ctx context.Context, storagePath, rev, treePath string, opts gitbackend.ListTreeOptions) (gitbackend.TreeListing, error) {
	if f.listFn != nil {
		f.listFn(storagePath, rev, treePath, opts)
	}
	return f.listing, f.listErr
}

func (f fakeStorage) Diff(ctx context.Context, storagePath, base, head string) (gitbackend.DiffResult, error) {
	return f.diffResult, f.diffErr
}

func (f fakeStorage) GetCommit(ctx context.Context, storagePath, sha string) (gitbackend.CommitDetails, error) {
	if f.commitFn != nil {
		f.commitFn(storagePath, sha)
	}
	return f.commitDetails, f.commitErr
}

func (f fakeStorage) ArchiveTar(ctx context.Context, storagePath, rev string, out io.Writer) (string, error) {
	if _, err := out.Write(f.tarBytes); err != nil {
		return "", err
	}
	return f.sha, nil
}

func (f fakeStorage) StatBlob(ctx context.Context, storagePath, rev, treePath string) (gitbackend.BlobInfo, error) {
	if f.blobStatErr != nil {
		return gitbackend.BlobInfo{}, f.blobStatErr
	}
	return f.blobInfo, nil
}

func (f fakeStorage) ReadBlob(ctx context.Context, storagePath, blobSHA string, out io.Writer) error {
	_, err := io.WriteString(out, f.blobContent)
	return err
}

func (f fakeStorage) WriteBlob(ctx context.Context, storagePath string, r io.Reader) (string, int64, error) {
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		return "", 0, err
	}
	return f.writeBlobSHA, n, nil
}

func (f fakeStorage) ApplyCommit(ctx context.Context, storagePath string, spec gitbackend.CommitSpec, ops []gitbackend.CommitOp, clean gitbackend.CleanFunc, checkWrite gitbackend.CheckWriteFunc) (gitbackend.RefChange, error) {
	for _, path := range f.checkWritePath {
		if checkWrite != nil {
			if err := checkWrite(path); err != nil {
				return gitbackend.RefChange{}, err
			}
		}
	}
	if f.applyFn != nil {
		if err := f.applyFn(spec, ops, clean); err != nil {
			return gitbackend.RefChange{}, err
		}
	}
	if f.applyErr != nil {
		return gitbackend.RefChange{}, f.applyErr
	}
	return f.applyChange, nil
}

type fakeLFS struct {
	objects map[string]string // oid -> content
	stored  map[string]string // oid -> content received via StoreObject
}

func stringPtr(value string) *string { return &value }

func (f fakeLFS) GetObject(ctx context.Context, repo domain.Repository, oid string) (io.ReadCloser, int64, error) {
	content, ok := f.objects[oid]
	if !ok {
		return nil, 0, lfsservice.ErrObjectNotFound
	}
	return io.NopCloser(strings.NewReader(content)), int64(len(content)), nil
}

func (f fakeLFS) StoreObject(ctx context.Context, repo domain.Repository, uploaderID int64, oid string, size int64, r io.Reader) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if f.stored != nil {
		f.stored[oid] = string(body)
	}
	return nil
}

type fakeDispatcher struct {
	events *[]domain.RepositoryEvent
}

func (f fakeDispatcher) DispatchEvent(ctx context.Context, event domain.RepositoryEvent) error {
	*f.events = append(*f.events, event)
	return nil
}

func TestCreateRepository(t *testing.T) {
	row := gen.Repository{ID: 7, OwnerID: 3, RepositoryName: "myrepo", StoragePath: "3/myrepo.git", Visibility: "private"}
	info := domain.RepositoryInfo{RepositoryName: "myrepo", Visibility: domain.RepoVisibilityPrivate}

	t.Run("ok", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, nil, nil)
		repo, err := svc.Create(context.Background(), 3, info)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.ID != 7 || repo.RepositoryName != "myrepo" {
			t.Errorf("unexpected repo: %+v", repo)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		// the insert is "on conflict do nothing returning *" -> on duplicate "no rows"
		svc := NewService(zap.NewNop(), fakeRegistry{createRepoErr: sql.ErrNoRows}, fakeStorage{}, nil, nil)
		if _, err := svc.Create(context.Background(), 3, info); !errors.Is(err, ErrRepositoryExists) {
			t.Errorf("want ErrRepositoryExists, got %v", err)
		}
	})

	t.Run("invalid name", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, nil, nil)
		if _, err := svc.Create(context.Background(), 3, domain.RepositoryInfo{RepositoryName: "../evil", Visibility: domain.RepoVisibilityPrivate}); !errors.Is(err, ErrInvalidRepositoryName) {
			t.Errorf("want ErrInvalidRepositoryName, got %v", err)
		}
	})
}

func TestContents(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	committedAt := time.Date(2026, 7, 30, 18, 42, 0, 0, time.UTC)
	lastCommit := gitbackend.CommitSummary{
		SHA:         testSHA,
		Message:     "Change difficulty",
		CommittedAt: committedAt,
	}
	listing := gitbackend.TreeListing{
		CommitSHA: testSHA,
		Entries: []gitbackend.TreeEntry{{
			Mode:       "100644",
			Type:       "blob",
			SHA:        "1111222233334444555566667777888899990000",
			Size:       192,
			Path:       "config/server.properties",
			LastCommit: &lastCommit,
		}},
	}

	var called bool
	storage := fakeStorage{
		listing: listing,
		listFn: func(storagePath, rev, treePath string, opts gitbackend.ListTreeOptions) {
			called = true
			if storagePath != row.StoragePath || rev != "main" || treePath != "config" || !opts.IncludeLastCommit {
				t.Errorf("ListTree(%q, %q, %q, %+v)", storagePath, rev, treePath, opts)
			}
		},
	}
	svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, storage, nil, nil)
	got, err := svc.Contents(context.Background(), row.ID, "main", "config", domain.ContentsOptions{IncludeLastCommit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("ListTree was not called")
	}
	if got.Ref != "main" || got.CommitSHA != testSHA || got.Path != "config" || len(got.Entries) != 1 {
		t.Fatalf("Contents = %+v", got)
	}
	entry := got.Entries[0]
	if entry.Name != "server.properties" || entry.Type != domain.TreeEntryFile || entry.Size != 192 {
		t.Errorf("entry = %+v", entry)
	}
	if entry.LastCommit == nil || entry.LastCommit.SHA != testSHA || entry.LastCommit.Message != "Change difficulty" || !entry.LastCommit.CommittedAt.Equal(committedAt) {
		t.Errorf("last commit = %+v", entry.LastCommit)
	}

	for _, tc := range []struct {
		name    string
		regErr  error
		listErr error
		want    error
	}{
		{"repository not found", sql.ErrNoRows, nil, ErrRepositoryNotFound},
		{"invalid ref", nil, gitbackend.ErrInvalidRev, ErrInvalidRef},
		{"ref not found", nil, gitbackend.ErrRevNotFound, ErrRefNotFound},
		{"invalid path", nil, gitbackend.ErrInvalidPath, ErrInvalidPath},
		{"path not found", nil, gitbackend.ErrPathNotFound, ErrPathNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(
				zap.NewNop(),
				fakeRegistry{repo: row, err: tc.regErr},
				fakeStorage{listErr: tc.listErr},
				nil,
				nil,
			)
			if _, err := svc.Contents(context.Background(), row.ID, "main", "", domain.ContentsOptions{}); !errors.Is(err, tc.want) {
				t.Errorf("Contents error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	headSHA := "1111222233334444555566667777888899990000"
	patch := "diff --git a/old.txt b/new.txt\n"
	result := gitbackend.DiffResult{
		BaseSHA: testSHA,
		HeadSHA: headSHA,
		Files: []gitbackend.DiffFile{{
			Status:     gitbackend.DiffRenamed,
			OldPath:    "old.txt",
			NewPath:    "new.txt",
			OldBlobSHA: "2222333344445555666677778888999900001111",
			NewBlobSHA: "3333444455556666777788889999000011112222",
			OldMode:    "100644",
			NewMode:    "100755",
			Additions:  2,
			Deletions:  1,
			Patch:      &patch,
		}},
	}
	svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{diffResult: result}, nil, nil)
	got, err := svc.Diff(context.Background(), row.ID, "main~1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseSHA != testSHA || got.HeadSHA != headSHA || len(got.Files) != 1 {
		t.Fatalf("Diff = %+v", got)
	}
	if file := got.Files[0]; file.Status != domain.DiffRenamed || file.OldPath != "old.txt" || file.NewPath != "new.txt" ||
		file.OldBlobSHA == "" || file.NewBlobSHA == "" || file.Additions != 2 || file.Deletions != 1 ||
		file.Patch == nil || *file.Patch != patch {
		t.Errorf("diff file = %+v", file)
	}

	for _, tc := range []struct {
		name    string
		regErr  error
		diffErr error
		want    error
	}{
		{"repository not found", sql.ErrNoRows, nil, ErrRepositoryNotFound},
		{"invalid ref", nil, gitbackend.ErrInvalidRev, ErrInvalidRef},
		{"ref not found", nil, gitbackend.ErrRevNotFound, ErrRefNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(
				zap.NewNop(),
				fakeRegistry{repo: row, err: tc.regErr},
				fakeStorage{diffErr: tc.diffErr},
				nil,
				nil,
			)
			if _, err := svc.Diff(context.Background(), row.ID, "base", "head"); !errors.Is(err, tc.want) {
				t.Errorf("Diff error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGetCommit(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	authoredAt := time.Date(2026, 7, 30, 18, 40, 0, 0, time.UTC)
	committedAt := time.Date(2026, 7, 30, 18, 42, 0, 0, time.UTC)
	parentSHA := strings.Repeat("b", 40)
	details := gitbackend.CommitDetails{
		SHA:         testSHA,
		Parents:     []string{parentSHA},
		Message:     "Update server configuration",
		Author:      gitbackend.Identity{Name: "Alex Developer", Email: "alex@example.com"},
		AuthoredAt:  authoredAt,
		CommittedAt: committedAt,
	}

	var called bool
	storage := fakeStorage{
		commitDetails: details,
		commitFn: func(storagePath, sha string) {
			called = true
			if storagePath != row.StoragePath || sha != testSHA {
				t.Errorf("GetCommit(%q, %q)", storagePath, sha)
			}
		},
	}
	svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, storage, nil, nil)
	got, err := svc.GetCommit(context.Background(), row.ID, testSHA)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("GetCommit was not called")
	}
	if got.SHA != testSHA || len(got.Parents) != 1 || got.Parents[0] != parentSHA ||
		got.Message != details.Message || got.Author.Name != details.Author.Name || got.Author.Email != details.Author.Email ||
		!got.AuthoredAt.Equal(authoredAt) || !got.CommittedAt.Equal(committedAt) {
		t.Errorf("commit = %+v", got)
	}

	for _, tc := range []struct {
		name      string
		regErr    error
		commitErr error
		want      error
	}{
		{"repository not found", sql.ErrNoRows, nil, ErrRepositoryNotFound},
		{"invalid sha", nil, gitbackend.ErrInvalidRev, ErrInvalidCommitSHA},
		{"commit not found", nil, gitbackend.ErrRevNotFound, ErrCommitNotFound},
		{"backend failure", nil, io.ErrUnexpectedEOF, io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(
				zap.NewNop(),
				fakeRegistry{repo: row, err: tc.regErr},
				fakeStorage{commitErr: tc.commitErr},
				nil,
				nil,
			)
			if _, err := svc.GetCommit(context.Background(), row.ID, testSHA); !errors.Is(err, tc.want) {
				t.Errorf("GetCommit error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPrepareArchive(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	customPrefix := "release/source"
	trailingPrefix := "release/source/"
	emptyPrefix := ""
	invalidPrefix := "../release"

	cases := []struct {
		name       string
		registry   Registry
		storage    RepositoryStorage
		lfs        LFSObjects
		format     string
		includeLFS bool
		prefix     *string
		wantPrefix string
		wantErr    error
	}{
		{"unsupported format", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "rar", false, nil, "", ErrUnsupportedFormat},
		{"invalid prefix", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", false, &invalidPrefix, "", ErrInvalidArchivePrefix},
		{"lfs disabled", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", true, nil, "", ErrLFSNotEnabled},
		{"lfs enabled ok", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, fakeLFS{}, "zip", true, nil, "myrepo-aaaabbbbcccc/", nil},
		{"repo not found", fakeRegistry{err: sql.ErrNoRows}, fakeStorage{sha: testSHA}, nil, "zip", false, nil, "", ErrRepositoryNotFound},
		{"invalid ref", fakeRegistry{repo: row}, fakeStorage{resolveErr: gitbackend.ErrInvalidRev}, nil, "zip", false, nil, "", ErrInvalidRef},
		{"ref not found", fakeRegistry{repo: row}, fakeStorage{resolveErr: gitbackend.ErrRevNotFound}, nil, "zip", false, nil, "", ErrRefNotFound},
		{"default prefix", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", false, nil, "myrepo-aaaabbbbcccc/", nil},
		{"custom prefix", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", false, &customPrefix, "release/source/", nil},
		{"normalized prefix", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", false, &trailingPrefix, "release/source/", nil},
		{"empty prefix", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", false, &emptyPrefix, "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(zap.NewNop(), tc.registry, tc.storage, tc.lfs, nil)
			req, err := svc.PrepareArchive(context.Background(), row.ID, "main", tc.format, tc.includeLFS, tc.prefix)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("PrepareArchive error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if req.CommitSHA != testSHA || req.Repository.ID != row.ID || req.Format != domain.ArchiveFormatZip || req.Prefix != tc.wantPrefix {
					t.Errorf("PrepareArchive = %+v", req)
				}
				if want := "myrepo-aaaabbbbcccc.zip"; req.Filename() != want {
					t.Errorf("Filename = %q, want %q", req.Filename(), want)
				}
			}
		})
	}
}

func TestStreamArchiveSmudgesLFS(t *testing.T) {
	oid := strings.Repeat("ab", 32)
	content := "REAL LFS CONTENT"
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, len(content))

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range []struct{ name, body string }{
		{"README.md", "hello\n"},
		{"big.bin", pointer},
	} {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: e.name, Mode: 0o644, Size: int64(len(e.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	svc := NewService(
		zap.NewNop(),
		fakeRegistry{},
		fakeStorage{sha: testSHA, tarBytes: tarBuf.Bytes()},
		fakeLFS{objects: map[string]string{oid: content}},
		nil,
	)

	req := domain.ArchiveRequest{
		Repository: domain.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git"},
		CommitSHA:  testSHA,
		Format:     domain.ArchiveFormatZip,
		IncludeLFS: true,
		Prefix:     "myrepo-aaaabbbbcccc/",
	}

	var out bytes.Buffer
	if err := svc.StreamArchive(context.Background(), req, &out); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = string(body)
	}

	prefix := "myrepo-aaaabbbbcccc/"
	if got[prefix+"big.bin"] != content {
		t.Errorf("big.bin not smudged: %q", got[prefix+"big.bin"])
	}
	if got[prefix+"README.md"] != "hello\n" {
		t.Errorf("README.md = %q", got[prefix+"README.md"])
	}
}

const blobSHA = "1111222233334444555566667777888899990000"

func blobStorage(content string) fakeStorage {
	return fakeStorage{
		blobInfo:    gitbackend.BlobInfo{CommitSHA: testSHA, BlobSHA: blobSHA, Size: int64(len(content))},
		blobContent: content,
	}
}

func TestPrepareBlob(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	oid := strings.Repeat("cd", 32)
	content := "REAL LFS CONTENT"
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, len(content))

	t.Run("raw file", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage("hello\n"), nil, nil)
		req, err := svc.PrepareBlob(context.Background(), row.ID, "main", "README.md", false)
		if err != nil {
			t.Fatal(err)
		}
		if req.BlobSHA != blobSHA || req.CommitSHA != testSHA || req.Size != 6 || req.LFSOID != "" {
			t.Errorf("PrepareBlob = %+v", req)
		}
	})

	t.Run("pointer smudged", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage(pointer), fakeLFS{objects: map[string]string{oid: content}}, nil)
		req, err := svc.PrepareBlob(context.Background(), row.ID, "main", "big.bin", true)
		if err != nil {
			t.Fatal(err)
		}
		if req.LFSOID != oid {
			t.Errorf("LFSOID = %q", req.LFSOID)
		}
		if req.Size != int64(len(content)) {
			t.Errorf("Size = %d, want object size %d", req.Size, len(content))
		}
	})

	t.Run("pointer without lfs flag stays raw", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage(pointer), fakeLFS{objects: map[string]string{oid: content}}, nil)
		req, err := svc.PrepareBlob(context.Background(), row.ID, "main", "big.bin", false)
		if err != nil {
			t.Fatal(err)
		}
		if req.LFSOID != "" || req.Size != int64(len(pointer)) {
			t.Errorf("PrepareBlob = %+v", req)
		}
	})

	t.Run("large blob is never sniffed", func(t *testing.T) {
		st := blobStorage(pointer)
		st.blobInfo.Size = 5000 // over the pointer cap, content must not be read
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, fakeLFS{}, nil)
		req, err := svc.PrepareBlob(context.Background(), row.ID, "main", "big.bin", true)
		if err != nil {
			t.Fatal(err)
		}
		if req.LFSOID != "" || req.Size != 5000 {
			t.Errorf("PrepareBlob = %+v", req)
		}
	})

	t.Run("missing lfs object fails loudly", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage(pointer), fakeLFS{}, nil)
		if _, err := svc.PrepareBlob(context.Background(), row.ID, "main", "big.bin", true); !errors.Is(err, ErrLFSObjectNotFound) {
			t.Errorf("want ErrLFSObjectNotFound, got %v", err)
		}
	})

	t.Run("errors", func(t *testing.T) {
		cases := []struct {
			name       string
			storage    RepositoryStorage
			lfs        LFSObjects
			includeLFS bool
			wantErr    error
		}{
			{"lfs disabled", blobStorage(""), nil, true, ErrLFSNotEnabled},
			{"not a file", fakeStorage{blobStatErr: gitbackend.ErrNotABlob}, nil, false, ErrNotAFile},
			{"path not found", fakeStorage{blobStatErr: gitbackend.ErrPathNotFound}, nil, false, ErrPathNotFound},
			{"ref not found", fakeStorage{blobStatErr: gitbackend.ErrRevNotFound}, nil, false, ErrRefNotFound},
			{"invalid ref", fakeStorage{blobStatErr: gitbackend.ErrInvalidRev}, nil, false, ErrInvalidRef},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, tc.storage, tc.lfs, nil)
				if _, err := svc.PrepareBlob(context.Background(), row.ID, "main", "x", tc.includeLFS); !errors.Is(err, tc.wantErr) {
					t.Errorf("PrepareBlob error = %v, want %v", err, tc.wantErr)
				}
			})
		}
	})
}

func TestStreamBlob(t *testing.T) {
	oid := strings.Repeat("cd", 32)
	content := "REAL LFS CONTENT"
	repo := domain.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git"}

	t.Run("raw", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{}, blobStorage("hello\n"), nil, nil)
		var out bytes.Buffer
		if err := svc.StreamBlob(context.Background(), domain.BlobRequest{Repository: repo, BlobSHA: blobSHA}, &out); err != nil {
			t.Fatal(err)
		}
		if out.String() != "hello\n" {
			t.Errorf("content = %q", out.String())
		}
	})

	t.Run("smudged", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{}, blobStorage(""), fakeLFS{objects: map[string]string{oid: content}}, nil)
		var out bytes.Buffer
		if err := svc.StreamBlob(context.Background(), domain.BlobRequest{Repository: repo, BlobSHA: blobSHA, LFSOID: oid}, &out); err != nil {
			t.Fatal(err)
		}
		if out.String() != content {
			t.Errorf("content = %q", out.String())
		}
	})
}

func TestCommit(t *testing.T) {
	row := gen.Repository{ID: 7, OwnerID: 3, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	change := gitbackend.RefChange{Ref: "refs/heads/main", OldSHA: strings.Repeat("a", 40), NewSHA: testSHA}
	req := domain.CommitRequest{
		Branch:          "main",
		Message:         "update",
		Author:          domain.CommitIdentity{Name: "api-user", Email: "api@test"},
		ExpectedHeadSHA: strings.Repeat("a", 40),
		PusherID:        42,
		Operations: []domain.CommitFileOp{
			{Path: "run.sh", BlobSHA: stringPtr(blobSHA), Executable: true},
			{Path: "old.txt", Delete: true},
			{MoveFrom: "plugins", Path: "server/plugins"},
		},
	}

	t.Run("maps ops and dispatches the push event", func(t *testing.T) {
		var events []domain.RepositoryEvent
		var gotSpec gitbackend.CommitSpec
		var gotOps []gitbackend.CommitOp
		var gotClean gitbackend.CleanFunc
		st := fakeStorage{applyChange: change, applyFn: func(spec gitbackend.CommitSpec, ops []gitbackend.CommitOp, clean gitbackend.CleanFunc) error {
			gotSpec, gotOps, gotClean = spec, ops, clean
			return nil
		}}

		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, fakeLFS{}, fakeDispatcher{events: &events})
		res, err := svc.Commit(context.Background(), row.ID, req)
		if err != nil {
			t.Fatal(err)
		}

		if res.CommitSHA != testSHA || res.Before != change.OldSHA || res.Branch != "main" {
			t.Errorf("result = %+v", res)
		}
		if gotSpec.Branch != "main" || gotSpec.ExpectedOld != req.ExpectedHeadSHA || gotSpec.Author.Name != "api-user" {
			t.Errorf("spec = %+v", gotSpec)
		}
		if len(gotOps) != 3 || gotOps[0].Mode != "100755" || !gotOps[1].Delete ||
			gotOps[2].MoveFrom != "plugins" || gotOps[2].Path != "server/plugins" {
			t.Errorf("ops = %+v", gotOps)
		}
		if gotClean == nil {
			t.Error("clean must be set when lfs is enabled")
		}

		if len(events) != 1 {
			t.Fatalf("events = %+v", events)
		}
		e := events[0]
		if e.Event != "push" || e.RepositoryFullName != "3/myrepo" || e.PusherID != 42 ||
			e.PusherUsername != "api-user" || e.Ref != change.Ref || e.OldSHA != change.OldSHA || e.NewSHA != change.NewSHA {
			t.Errorf("event = %+v", e)
		}
	})

	t.Run("nil clean when lfs disabled", func(t *testing.T) {
		st := fakeStorage{applyChange: change, applyFn: func(_ gitbackend.CommitSpec, _ []gitbackend.CommitOp, clean gitbackend.CleanFunc) error {
			if clean != nil {
				t.Error("clean must be nil when lfs is disabled")
			}
			return nil
		}}
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, nil, nil)
		if _, err := svc.Commit(context.Background(), row.ID, req); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("explicit lfs object requires lfs service", func(t *testing.T) {
		lfsReq := req
		lfsReq.Operations = []domain.CommitFileOp{{
			Path: "model.bin",
			Lfs:  &domain.CommitFileLfsObject{OID: strings.Repeat("a", 64), Size: 42},
		}}
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, nil, nil)
		if _, err := svc.Commit(context.Background(), row.ID, lfsReq); !errors.Is(err, ErrLFSNotEnabled) {
			t.Fatalf("want ErrLFSNotEnabled, got %v", err)
		}
	})

	t.Run("maps a verified explicit lfs object", func(t *testing.T) {
		oid := strings.Repeat("a", 64)
		lfsReq := req
		lfsReq.Operations = []domain.CommitFileOp{{
			Path: "model.bin",
			Lfs:  &domain.CommitFileLfsObject{OID: oid, Size: 42},
		}}
		st := fakeStorage{applyChange: change, applyFn: func(_ gitbackend.CommitSpec, ops []gitbackend.CommitOp, _ gitbackend.CleanFunc) error {
			if len(ops) != 1 || ops[0].BlobSHA != "" || ops[0].Lfs == nil || ops[0].Lfs.OID != oid || ops[0].Lfs.Size != 42 {
				t.Errorf("backend ops = %+v", ops)
			}
			return nil
		}}
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, fakeLFS{objects: map[string]string{oid: strings.Repeat("x", 42)}}, nil)
		if _, err := svc.Commit(context.Background(), row.ID, lfsReq); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rejects unavailable or mismatched lfs object", func(t *testing.T) {
		oid := strings.Repeat("a", 64)
		lfsReq := req
		lfsReq.Operations = []domain.CommitFileOp{{
			Path: "model.bin",
			Lfs:  &domain.CommitFileLfsObject{OID: oid, Size: 42},
		}}

		missing := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, fakeLFS{}, nil)
		if _, err := missing.Commit(context.Background(), row.ID, lfsReq); !errors.Is(err, ErrLFSObjectNotFound) {
			t.Fatalf("missing object: want ErrLFSObjectNotFound, got %v", err)
		}

		mismatch := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, fakeLFS{objects: map[string]string{oid: "short"}}, nil)
		if _, err := mismatch.Commit(context.Background(), row.ID, lfsReq); !errors.Is(err, ErrInvalidCommitOps) {
			t.Fatalf("size mismatch: want ErrInvalidCommitOps, got %v", err)
		}
	})

	t.Run("error mapping and no event on failure", func(t *testing.T) {
		cases := []struct {
			backend error
			want    error
		}{
			{gitbackend.ErrInvalidBranch, ErrInvalidBranch},
			{gitbackend.ErrInvalidOps, ErrInvalidCommitOps},
			{gitbackend.ErrRevNotFound, ErrRefNotFound},
			{gitbackend.ErrPathNotFound, ErrPathNotFound},
			{gitbackend.ErrPathExists, ErrPathConflict},
			{gitbackend.ErrNotABlob, ErrNotAFile},
			{gitbackend.ErrHeadMismatch, ErrHeadMismatch},
			{gitbackend.ErrUnknownBlob, ErrUnknownBlob},
			{gitbackend.ErrNothingToCommit, ErrNothingToCommit},
			{gitbackend.ErrLFSRequired, ErrLFSNotEnabled},
			{gitbackend.ErrLFSNotTracked, ErrInvalidCommitOps},
		}
		for _, tc := range cases {
			var events []domain.RepositoryEvent
			svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{applyErr: tc.backend}, nil, fakeDispatcher{events: &events})
			if _, err := svc.Commit(context.Background(), row.ID, req); !errors.Is(err, tc.want) {
				t.Errorf("backend %v: got %v, want %v", tc.backend, err, tc.want)
			}
			if len(events) != 0 {
				t.Errorf("backend %v: event dispatched on failure", tc.backend)
			}
		}
	})
}

func TestCommitCleanClosure(t *testing.T) {
	row := gen.Repository{ID: 7, OwnerID: 3, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	pointerBlob := strings.Repeat("f", 40)
	req := domain.CommitRequest{
		Branch:  "main",
		Message: "x",
		Author:  domain.CommitIdentity{Name: "t", Email: "t@t"},
		Operations: []domain.CommitFileOp{
			{Path: "big.bin", BlobSHA: stringPtr(blobSHA)},
		},
	}

	t.Run("converts payload to lfs object and pointer", func(t *testing.T) {
		payload := "REAL BINARY PAYLOAD"
		wantOID := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))

		stored := map[string]string{}
		st := fakeStorage{
			blobContent:  payload,
			writeBlobSHA: pointerBlob,
			applyFn: func(_ gitbackend.CommitSpec, _ []gitbackend.CommitOp, clean gitbackend.CleanFunc) error {
				got, err := clean("big.bin", blobSHA, int64(len(payload)))
				if err != nil {
					return err
				}
				if got != pointerBlob {
					t.Errorf("clean returned %q, want pointer blob %q", got, pointerBlob)
				}
				return nil
			},
		}

		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, fakeLFS{stored: stored}, nil)
		if _, err := svc.Commit(context.Background(), row.ID, req); err != nil {
			t.Fatal(err)
		}
		if stored[wantOID] != payload {
			t.Errorf("stored objects = %v, want oid %s with payload", stored, wantOID)
		}
	})

	t.Run("existing pointer passes through untouched", func(t *testing.T) {
		pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("ab", 32) + "\nsize 44\n"

		stored := map[string]string{}
		st := fakeStorage{
			blobContent: pointer,
			applyFn: func(_ gitbackend.CommitSpec, _ []gitbackend.CommitOp, clean gitbackend.CleanFunc) error {
				got, err := clean("big.bin", blobSHA, int64(len(pointer)))
				if err != nil {
					return err
				}
				if got != blobSHA {
					t.Errorf("clean returned %q, want passthrough %q", got, blobSHA)
				}
				return nil
			},
		}

		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, fakeLFS{stored: stored}, nil)
		if _, err := svc.Commit(context.Background(), row.ID, req); err != nil {
			t.Fatal(err)
		}
		if len(stored) != 0 {
			t.Errorf("pointer passthrough must not store objects, got %v", stored)
		}
	})
}

func TestAddPathPolicy(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}

	t.Run("normalizes and stores", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, nil, nil)
		p, err := svc.AddPathPolicy(context.Background(), row.ID, "/runtime/", "deploy state")
		if err != nil {
			t.Fatal(err)
		}
		if p.Pattern != "runtime" || p.Kind != domain.PathPolicyBlock || p.Reason != "deploy state" {
			t.Errorf("policy = %+v", p)
		}
	})

	t.Run("invalid pattern", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, fakeStorage{}, nil, nil)
		if _, err := svc.AddPathPolicy(context.Background(), row.ID, "a/../b", ""); !errors.Is(err, ErrInvalidPathPattern) {
			t.Errorf("want ErrInvalidPathPattern, got %v", err)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row, createPolicyErr: sql.ErrNoRows}, fakeStorage{}, nil, nil)
		if _, err := svc.AddPathPolicy(context.Background(), row.ID, "runtime", ""); !errors.Is(err, ErrPathPolicyExists) {
			t.Errorf("want ErrPathPolicyExists, got %v", err)
		}
	})

	t.Run("repo not found", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{err: sql.ErrNoRows}, fakeStorage{}, nil, nil)
		if _, err := svc.AddPathPolicy(context.Background(), 404, "runtime", ""); !errors.Is(err, ErrRepositoryNotFound) {
			t.Errorf("want ErrRepositoryNotFound, got %v", err)
		}
	})
}

func TestCommitPathPolicies(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}
	policies := []gen.PathPolicy{
		{ID: 1, RepositoryID: 7, Pattern: "runtime", Kind: "block", Reason: sql.NullString{String: "deploy-managed state", Valid: true}},
		{ID: 2, RepositoryID: 7, Pattern: "config.lock", Kind: "block"},
	}
	base := domain.CommitRequest{
		Branch:  "main",
		Message: "x",
		Author:  domain.CommitIdentity{Name: "t", Email: "t@t"},
	}
	change := gitbackend.RefChange{Ref: "refs/heads/main", OldSHA: strings.Repeat("a", 40), NewSHA: testSHA}

	cases := []struct {
		name    string
		ops     []domain.CommitFileOp
		blocked bool
	}{
		{"put inside blocked dir", []domain.CommitFileOp{{Path: "runtime/state.json", BlobSHA: stringPtr(blobSHA)}}, true},
		{"put blocked file", []domain.CommitFileOp{{Path: "config.lock", BlobSHA: stringPtr(blobSHA)}}, true},
		{"dot-segment evasion", []domain.CommitFileOp{{Path: "./runtime/state.json", BlobSHA: stringPtr(blobSHA)}}, true},
		{"delete of blocked path allowed", []domain.CommitFileOp{{Path: "runtime/state.json", Delete: true}}, false},
		{"unrelated put", []domain.CommitFileOp{{Path: "src/main.go", BlobSHA: stringPtr(blobSHA)}}, false},
		{"sibling prefix not blocked", []domain.CommitFileOp{{Path: "runtimes/x", BlobSHA: stringPtr(blobSHA)}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applied := false
			st := fakeStorage{applyChange: change, applyFn: func(_ gitbackend.CommitSpec, _ []gitbackend.CommitOp, _ gitbackend.CleanFunc) error {
				applied = true
				return nil
			}}
			svc := NewService(zap.NewNop(), fakeRegistry{repo: row, policies: policies}, st, nil, nil)

			req := base
			req.Operations = tc.ops
			_, err := svc.Commit(context.Background(), row.ID, req)

			if tc.blocked {
				if !errors.Is(err, ErrPathBlocked) {
					t.Fatalf("want ErrPathBlocked, got %v", err)
				}
				if applied {
					t.Error("blocked commit must never reach the backend")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if !applied {
					t.Error("allowed commit did not reach the backend")
				}
			}
		})
	}

	t.Run("reason is echoed", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row, policies: policies}, fakeStorage{}, nil, nil)
		req := base
		req.Operations = []domain.CommitFileOp{{Path: "runtime/x", BlobSHA: stringPtr(blobSHA)}}
		_, err := svc.Commit(context.Background(), row.ID, req)
		if err == nil || !strings.Contains(err.Error(), "deploy-managed state") {
			t.Errorf("reason missing from error: %v", err)
		}
	})

	t.Run("move descendants are checked", func(t *testing.T) {
		st := fakeStorage{
			applyChange:    change,
			checkWritePath: []string{"runtime/moved/state.json"},
		}
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row, policies: policies}, st, nil, nil)
		req := base
		req.Operations = []domain.CommitFileOp{{MoveFrom: "plugins", Path: "runtime/moved"}}
		if _, err := svc.Commit(context.Background(), row.ID, req); !errors.Is(err, ErrPathBlocked) {
			t.Fatalf("want ErrPathBlocked, got %v", err)
		}
	})
}
