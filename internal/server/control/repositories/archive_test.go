package repositories

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
)

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
