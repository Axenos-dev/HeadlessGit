package repositories

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
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
}

func (f fakeRegistry) GetRepository(ctx context.Context, repositoryID int64) (gen.Repository, error) {
	return f.repo, f.err
}

type fakeStorage struct {
	RepositoryStorage
	sha        string
	resolveErr error
	tarBytes   []byte

	blobInfo    gitbackend.BlobInfo
	blobStatErr error
	blobContent string
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

type fakeLFS struct {
	objects map[string]string // oid -> content
}

func (f fakeLFS) GetObject(ctx context.Context, repo domain.Repository, oid string) (io.ReadCloser, int64, error) {
	content, ok := f.objects[oid]
	if !ok {
		return nil, 0, errors.New("object not found")
	}
	return io.NopCloser(strings.NewReader(content)), int64(len(content)), nil
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
			svc := NewService(zap.NewNop(), tc.registry, tc.storage, tc.lfs)
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
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage("hello\n"), nil)
		req, err := svc.PrepareBlob(context.Background(), row.ID, "main", "README.md", false)
		if err != nil {
			t.Fatal(err)
		}
		if req.BlobSHA != blobSHA || req.CommitSHA != testSHA || req.Size != 6 || req.LFSOID != "" {
			t.Errorf("PrepareBlob = %+v", req)
		}
	})

	t.Run("pointer smudged", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage(pointer), fakeLFS{objects: map[string]string{oid: content}})
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
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage(pointer), fakeLFS{objects: map[string]string{oid: content}})
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
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, st, fakeLFS{})
		req, err := svc.PrepareBlob(context.Background(), row.ID, "main", "big.bin", true)
		if err != nil {
			t.Fatal(err)
		}
		if req.LFSOID != "" || req.Size != 5000 {
			t.Errorf("PrepareBlob = %+v", req)
		}
	})

	t.Run("missing lfs object fails loudly", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, blobStorage(pointer), fakeLFS{})
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
				svc := NewService(zap.NewNop(), fakeRegistry{repo: row}, tc.storage, tc.lfs)
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
		svc := NewService(zap.NewNop(), fakeRegistry{}, blobStorage("hello\n"), nil)
		var out bytes.Buffer
		if err := svc.StreamBlob(context.Background(), domain.BlobRequest{Repository: repo, BlobSHA: blobSHA}, &out); err != nil {
			t.Fatal(err)
		}
		if out.String() != "hello\n" {
			t.Errorf("content = %q", out.String())
		}
	})

	t.Run("smudged", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{}, blobStorage(""), fakeLFS{objects: map[string]string{oid: content}})
		var out bytes.Buffer
		if err := svc.StreamBlob(context.Background(), domain.BlobRequest{Repository: repo, BlobSHA: blobSHA, LFSOID: oid}, &out); err != nil {
			t.Fatal(err)
		}
		if out.String() != content {
			t.Errorf("content = %q", out.String())
		}
	})
}
