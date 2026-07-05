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
