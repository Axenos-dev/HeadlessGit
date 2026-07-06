package repositories

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const testSHA = "aaaabbbbccccddddeeeeffff0000111122223333"

// fakeManager stubs RepositoryManager for handler tests: embed the interface
// and override only what the endpoint under test touches
type fakeManager struct {
	RepositoryManager
	prepareReq domain.ArchiveRequest
	prepareErr error
	streamBody string
	streamErr  error

	blobReq domain.BlobRequest
	blobErr error
}

func (f fakeManager) PrepareArchive(ctx context.Context, repositoryID int64, ref, format string, includeLFS bool) (domain.ArchiveRequest, error) {
	return f.prepareReq, f.prepareErr
}

func (f fakeManager) StreamArchive(ctx context.Context, req domain.ArchiveRequest, out io.Writer) error {
	if f.streamBody != "" {
		zw := zip.NewWriter(out)
		w, err := zw.Create("file.txt")
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(f.streamBody)); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
	}
	return f.streamErr
}

func (f fakeManager) PrepareBlob(ctx context.Context, repositoryID int64, ref, treePath string, includeLFS bool) (domain.BlobRequest, error) {
	return f.blobReq, f.blobErr
}

func (f fakeManager) StreamBlob(ctx context.Context, req domain.BlobRequest, out io.Writer) error {
	if f.streamBody != "" {
		if _, err := io.WriteString(out, f.streamBody); err != nil {
			return err
		}
	}
	return f.streamErr
}

// newTestRouter mounts the handlers the same way the control server does
func newTestRouter(svc RepositoryManager) http.Handler {
	r := chi.NewRouter()
	NewHandlers(zap.NewNop(), svc).RegisterRoutes(r)
	return r
}

func testArchiveRequest() domain.ArchiveRequest {
	return domain.ArchiveRequest{
		Repository: domain.Repository{ID: 7, RepositoryName: "myrepo"},
		CommitSHA:  testSHA,
		Format:     domain.ArchiveFormatZip,
	}
}

func TestGetArchive(t *testing.T) {
	router := newTestRouter(fakeManager{prepareReq: testArchiveRequest(), streamBody: "hello"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repositories/7/archive?ref=main", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="myrepo-aaaabbbbcccc.zip"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("ETag"); got != `W/"`+testSHA+`-zip"` {
		t.Errorf("ETag = %q", got)
	}
	if got := rec.Header().Get("X-HeadlessGit-Commit"); got != testSHA {
		t.Errorf("X-HeadlessGit-Commit = %q", got)
	}

	// the body must be a readable zip
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "file.txt" {
		t.Errorf("zip entries = %v", zr.File)
	}
}

func TestGetArchiveNotModified(t *testing.T) {
	router := newTestRouter(fakeManager{prepareReq: testArchiveRequest(), streamBody: "hello"})

	req := httptest.NewRequest(http.MethodGet, "/repositories/7/archive?ref=main", nil)
	req.Header.Set("If-None-Match", `W/"`+testSHA+`-zip"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 must have no body, got %d bytes", rec.Body.Len())
	}
}

func TestGetArchiveErrors(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		prepareErr error
		streamErr  error
		wantStatus int
		wantCode   string
	}{
		{"bad id", "/repositories/abc/archive", nil, nil, http.StatusBadRequest, "invalid_request"},
		{"bad lfs param", "/repositories/7/archive?lfs=maybe", nil, nil, http.StatusBadRequest, "invalid_request"},
		{"repo not found", "/repositories/7/archive", reposervice.ErrRepositoryNotFound, nil, http.StatusNotFound, "repository_not_found"},
		{"ref not found", "/repositories/7/archive?ref=nope", reposervice.ErrRefNotFound, nil, http.StatusNotFound, "ref_not_found"},
		{"invalid ref", "/repositories/7/archive?ref=--x", reposervice.ErrInvalidRef, nil, http.StatusBadRequest, "invalid_request"},
		{"bad format", "/repositories/7/archive?format=rar", reposervice.ErrUnsupportedFormat, nil, http.StatusBadRequest, "invalid_request"},
		{"lfs disabled", "/repositories/7/archive?lfs=true", reposervice.ErrLFSNotEnabled, nil, http.StatusBadRequest, "invalid_request"},
		{"stream fails before first byte", "/repositories/7/archive", nil, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(fakeManager{prepareReq: testArchiveRequest(), prepareErr: tc.prepareErr, streamErr: tc.streamErr})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.wantCode)
			}
			if got := rec.Header().Get("Content-Disposition"); got != "" {
				t.Errorf("error response leaked Content-Disposition %q", got)
			}
		})
	}
}

func testBlobRequest(lfsOID string) domain.BlobRequest {
	return domain.BlobRequest{
		Repository: domain.Repository{ID: 7, RepositoryName: "myrepo"},
		CommitSHA:  testSHA,
		BlobSHA:    "1111222233334444555566667777888899990000",
		Path:       "src/main.go",
		Size:       6,
		LFSOID:     lfsOID,
	}
}

func TestGetBlob(t *testing.T) {
	router := newTestRouter(fakeManager{blobReq: testBlobRequest(""), streamBody: "hello\n"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repositories/7/blob?ref=main&path=src/main.go", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello\n" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("ETag"); got != `"1111222233334444555566667777888899990000"` {
		t.Errorf("ETag = %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "6" {
		t.Errorf("Content-Length = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="main.go"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("X-HeadlessGit-Commit"); got != testSHA {
		t.Errorf("X-HeadlessGit-Commit = %q", got)
	}
}

func TestGetBlobLFSVariantETag(t *testing.T) {
	router := newTestRouter(fakeManager{blobReq: testBlobRequest("deadbeef"), streamBody: "hello\n"})

	// the raw etag must not satisfy a smudged request
	req := httptest.NewRequest(http.MethodGet, "/repositories/7/blob?ref=main&path=src/main.go&lfs=true", nil)
	req.Header.Set("If-None-Match", `"1111222233334444555566667777888899990000"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("raw etag must not match lfs variant, status = %d", rec.Code)
	}
	if got := rec.Header().Get("ETag"); got != `"1111222233334444555566667777888899990000-lfs"` {
		t.Errorf("ETag = %q", got)
	}
}

func TestGetBlobNotModified(t *testing.T) {
	router := newTestRouter(fakeManager{blobReq: testBlobRequest(""), streamBody: "hello\n"})

	req := httptest.NewRequest(http.MethodGet, "/repositories/7/blob?ref=main&path=src/main.go", nil)
	req.Header.Set("If-None-Match", `"1111222233334444555566667777888899990000"`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 must have no body, got %d bytes", rec.Body.Len())
	}
}

func TestGetBlobErrors(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		blobErr    error
		streamErr  error
		wantStatus int
		wantCode   string
	}{
		{"bad id", "/repositories/abc/blob", nil, nil, http.StatusBadRequest, "invalid_request"},
		{"bad lfs param", "/repositories/7/blob?lfs=maybe", nil, nil, http.StatusBadRequest, "invalid_request"},
		{"repo not found", "/repositories/7/blob", reposervice.ErrRepositoryNotFound, nil, http.StatusNotFound, "repository_not_found"},
		{"ref not found", "/repositories/7/blob?ref=nope", reposervice.ErrRefNotFound, nil, http.StatusNotFound, "ref_not_found"},
		{"path not found", "/repositories/7/blob?path=nope", reposervice.ErrPathNotFound, nil, http.StatusNotFound, "path_not_found"},
		{"lfs object missing", "/repositories/7/blob?lfs=true", reposervice.ErrLFSObjectNotFound, nil, http.StatusNotFound, "lfs_object_not_found"},
		{"path is a directory", "/repositories/7/blob?path=src", reposervice.ErrNotAFile, nil, http.StatusBadRequest, "invalid_request"},
		{"invalid ref", "/repositories/7/blob?ref=--x", reposervice.ErrInvalidRef, nil, http.StatusBadRequest, "invalid_request"},
		{"lfs disabled", "/repositories/7/blob?lfs=true", reposervice.ErrLFSNotEnabled, nil, http.StatusBadRequest, "invalid_request"},
		{"stream fails before first byte", "/repositories/7/blob", nil, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(fakeManager{blobReq: testBlobRequest(""), blobErr: tc.blobErr, streamErr: tc.streamErr})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.wantCode)
			}
			if got := rec.Header().Get("Content-Disposition"); got != "" {
				t.Errorf("error response leaked Content-Disposition %q", got)
			}
		})
	}
}
