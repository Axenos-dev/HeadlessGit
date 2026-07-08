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

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
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

	blobInfo    gitbackend.BlobInfo
	blobStatErr error
	blobContent string

	writeBlobSHA string
	applyChange  gitbackend.RefChange
	applyErr     error
	// optional hook to inspect (and exercise) what ApplyCommit received
	applyFn func(spec gitbackend.CommitSpec, ops []gitbackend.CommitOp, clean gitbackend.CleanFunc) error
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

func (f fakeStorage) ApplyCommit(ctx context.Context, storagePath string, spec gitbackend.CommitSpec, ops []gitbackend.CommitOp, clean gitbackend.CleanFunc) (gitbackend.RefChange, error) {
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

func (f fakeLFS) GetObject(ctx context.Context, repo domain.Repository, oid string) (io.ReadCloser, int64, error) {
	content, ok := f.objects[oid]
	if !ok {
		return nil, 0, errors.New("object not found")
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

func TestPrepareArchive(t *testing.T) {
	row := gen.Repository{ID: 7, RepositoryName: "myrepo", StoragePath: "7/myrepo.git", Visibility: "private"}

	cases := []struct {
		name       string
		registry   Registry
		storage    RepositoryStorage
		lfs        LFSObjects
		format     string
		includeLFS bool
		wantErr    error
	}{
		{"unsupported format", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "rar", false, ErrUnsupportedFormat},
		{"lfs disabled", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", true, ErrLFSNotEnabled},
		{"lfs enabled ok", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, fakeLFS{}, "zip", true, nil},
		{"repo not found", fakeRegistry{err: sql.ErrNoRows}, fakeStorage{sha: testSHA}, nil, "zip", false, ErrRepositoryNotFound},
		{"invalid ref", fakeRegistry{repo: row}, fakeStorage{resolveErr: gitbackend.ErrInvalidRev}, nil, "zip", false, ErrInvalidRef},
		{"ref not found", fakeRegistry{repo: row}, fakeStorage{resolveErr: gitbackend.ErrRevNotFound}, nil, "zip", false, ErrRefNotFound},
		{"ok", fakeRegistry{repo: row}, fakeStorage{sha: testSHA}, nil, "zip", false, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(zap.NewNop(), tc.registry, tc.storage, tc.lfs, nil)
			req, err := svc.PrepareArchive(context.Background(), row.ID, "main", tc.format, tc.includeLFS)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("PrepareArchive error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if req.CommitSHA != testSHA || req.Repository.ID != row.ID || req.Format != domain.ArchiveFormatZip {
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
			{Path: "run.sh", BlobSHA: blobSHA, Executable: true},
			{Path: "old.txt", Delete: true},
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
		if len(gotOps) != 2 || gotOps[0].Mode != "100755" || !gotOps[1].Delete {
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

	t.Run("error mapping and no event on failure", func(t *testing.T) {
		cases := []struct {
			backend error
			want    error
		}{
			{gitbackend.ErrInvalidBranch, ErrInvalidBranch},
			{gitbackend.ErrInvalidOps, ErrInvalidCommitOps},
			{gitbackend.ErrRevNotFound, ErrRefNotFound},
			{gitbackend.ErrPathNotFound, ErrPathNotFound},
			{gitbackend.ErrNotABlob, ErrNotAFile},
			{gitbackend.ErrHeadMismatch, ErrHeadMismatch},
			{gitbackend.ErrUnknownBlob, ErrUnknownBlob},
			{gitbackend.ErrNothingToCommit, ErrNothingToCommit},
			{gitbackend.ErrLFSRequired, ErrLFSNotEnabled},
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
			{Path: "big.bin", BlobSHA: blobSHA},
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
		{"put inside blocked dir", []domain.CommitFileOp{{Path: "runtime/state.json", BlobSHA: blobSHA}}, true},
		{"put blocked file", []domain.CommitFileOp{{Path: "config.lock", BlobSHA: blobSHA}}, true},
		{"dot-segment evasion", []domain.CommitFileOp{{Path: "./runtime/state.json", BlobSHA: blobSHA}}, true},
		{"delete of blocked path allowed", []domain.CommitFileOp{{Path: "runtime/state.json", Delete: true}}, false},
		{"unrelated put", []domain.CommitFileOp{{Path: "src/main.go", BlobSHA: blobSHA}}, false},
		{"sibling prefix not blocked", []domain.CommitFileOp{{Path: "runtimes/x", BlobSHA: blobSHA}}, false},
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
		req.Operations = []domain.CommitFileOp{{Path: "runtime/x", BlobSHA: blobSHA}}
		_, err := svc.Commit(context.Background(), row.ID, req)
		if err == nil || !strings.Contains(err.Error(), "deploy-managed state") {
			t.Errorf("reason missing from error: %v", err)
		}
	})
}
