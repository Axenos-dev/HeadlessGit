package repositories

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	tree     domain.RepositoryTree
	treeErr  error
	treeRef  string
	treePath string
	treeOpts domain.TreeOptions

	diffResult domain.RepositoryDiff
	diffErr    error
	diffBase   string
	diffHead   string

	commitDetails   domain.CommitDetails
	getCommitErr    error
	getCommitSHA    string
	getCommitRepoID int64

	prepareReq domain.ArchiveRequest
	prepareErr error
	prefix     string
	prefixSet  bool
	streamBody string
	streamErr  error

	fileInfo domain.FileInfo
	fileReq  domain.FileRequest
	fileErr  error

	uploadTarget       domain.UploadTarget
	uploadRepositoryID int64
	uploadUserID       int64
	uploadSize         int64

	commitResult domain.CommitResult
	commitErr    error
	commitReq    domain.CommitRequest
	commitCalled bool

	policy        domain.PathPolicy
	policyList    []domain.PathPolicy
	policyErr     error
	policyPattern string
	policyReason  string

	createdRepo domain.Repository
	createErr   error

	repoByPath    domain.Repository
	repoByPathErr error
}

func (f *fakeManager) Tree(ctx context.Context, repositoryID int64, ref, treePath string, opts domain.TreeOptions) (domain.RepositoryTree, error) {
	f.treeRef = ref
	f.treePath = treePath
	f.treeOpts = opts
	return f.tree, f.treeErr
}

func (f *fakeManager) Diff(ctx context.Context, repositoryID int64, base, head string) (domain.RepositoryDiff, error) {
	f.diffBase = base
	f.diffHead = head
	return f.diffResult, f.diffErr
}

func (f *fakeManager) GetCommit(ctx context.Context, repositoryID int64, sha string) (domain.CommitDetails, error) {
	f.getCommitRepoID = repositoryID
	f.getCommitSHA = sha
	return f.commitDetails, f.getCommitErr
}

func (f *fakeManager) Create(ctx context.Context, ownerID int64, info domain.RepositoryInfo) (domain.Repository, error) {
	return f.createdRepo, f.createErr
}

func (f *fakeManager) GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error) {
	return f.repoByPath, f.repoByPathErr
}

func (f *fakeManager) ListPathPolicies(ctx context.Context, repositoryID int64) ([]domain.PathPolicy, error) {
	return f.policyList, f.policyErr
}

func (f *fakeManager) AddPathPolicy(ctx context.Context, repositoryID int64, pattern, reason string) (domain.PathPolicy, error) {
	f.policyPattern, f.policyReason = pattern, reason
	return f.policy, f.policyErr
}

func (f *fakeManager) RemovePathPolicy(ctx context.Context, repositoryID, policyID int64) error {
	return f.policyErr
}

func (f *fakeManager) Commit(ctx context.Context, repositoryID int64, req domain.CommitRequest) (domain.CommitResult, error) {
	f.commitCalled = true
	f.commitReq = req
	return f.commitResult, f.commitErr
}

func (f *fakeManager) PrepareArchive(ctx context.Context, repositoryID int64, ref, format string, includeLFS bool, prefix *string) (domain.ArchiveRequest, error) {
	if prefix != nil {
		f.prefix = *prefix
		f.prefixSet = true
	}
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

func (f fakeManager) GetFile(ctx context.Context, repositoryID int64, blobSHA string) (domain.FileInfo, error) {
	return f.fileInfo, f.fileErr
}

func (f fakeManager) PrepareFile(ctx context.Context, repositoryID int64, blobSHA string) (domain.FileRequest, error) {
	return f.fileReq, f.fileErr
}

func (f fakeManager) StreamFile(ctx context.Context, req domain.FileRequest, out io.Writer) error {
	if f.streamBody != "" {
		if _, err := io.WriteString(out, f.streamBody); err != nil {
			return err
		}
	}
	return f.streamErr
}

func (f *fakeManager) CreateUpload(_ context.Context, repositoryID, userID, size int64) (domain.UploadTarget, error) {
	f.uploadRepositoryID = repositoryID
	f.uploadUserID = userID
	f.uploadSize = size
	return f.uploadTarget, nil
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
		Prefix:     "myrepo-aaaabbbbcccc/",
	}
}

func TestGetTree(t *testing.T) {
	committedAt := time.Date(2026, 7, 30, 18, 42, 0, 0, time.UTC)
	svc := &fakeManager{tree: domain.RepositoryTree{
		Ref:       "main",
		CommitSHA: testSHA,
		Path:      "config",
		Entry: domain.TreeNode{
			TreeEntry: domain.TreeEntry{
				Name: "config",
				Path: "config",
				Type: domain.TreeEntryDirectory,
				Mode: "040000",
				SHA:  "0000111122223333444455556666777788889999",
			},
			Entries: []domain.TreeEntry{{
				Name: "server.properties",
				Path: "config/server.properties",
				Type: domain.TreeEntryFile,
				Mode: "100644",
				SHA:  "1111222233334444555566667777888899990000",
				LastCommit: &domain.CommitSummary{
					SHA:         testSHA,
					Message:     "Change difficulty",
					CommittedAt: committedAt,
				},
			}},
		},
	}}

	rec := httptest.NewRecorder()
	newTestRouter(svc).ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet,
		"/repositories/7/tree?ref=main&path=config&include=lastCommit",
		nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if svc.treeRef != "main" || svc.treePath != "config" || !svc.treeOpts.IncludeLastCommit {
		t.Errorf("Tree args = ref %q, path %q, opts %+v", svc.treeRef, svc.treePath, svc.treeOpts)
	}

	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	var tree struct {
		Ref       string `json:"ref"`
		CommitSHA string `json:"commitSha"`
		Path      string `json:"path"`
		Entry     struct {
			Type    string `json:"type"`
			TreeSHA string `json:"treeSha"`
			BlobSHA string `json:"blobSha"`
			Entries []struct {
				Type       string         `json:"type"`
				Name       string         `json:"name"`
				BlobSHA    string         `json:"blobSha"`
				Size       *int64         `json:"size"`
				LastCommit *CommitSummary `json:"lastCommit"`
			} `json:"entries"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(body.Data, &tree); err != nil {
		t.Fatal(err)
	}
	if tree.Ref != "main" || tree.CommitSHA != testSHA || tree.Entry.Type != "directory" || tree.Entry.BlobSHA != "" {
		t.Fatalf("tree = %s", body.Data)
	}
	if len(tree.Entry.Entries) != 1 || tree.Entry.Entries[0].BlobSHA != "1111222233334444555566667777888899990000" {
		t.Fatalf("entries = %s", body.Data)
	}
	entry := tree.Entry.Entries[0]
	if entry.Size != nil {
		t.Errorf("tree listing included size %v", *entry.Size)
	}
	if entry.LastCommit == nil || entry.LastCommit.SHA != testSHA || entry.LastCommit.Message != "Change difficulty" || !entry.LastCommit.CommittedAt.Equal(committedAt) {
		t.Errorf("lastCommit = %+v", entry.LastCommit)
	}
}

func TestGetTreeErrors(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"bad id", "/repositories/nope/tree", nil, http.StatusBadRequest, "invalid_request"},
		{"bad include", "/repositories/7/tree?include=commits", nil, http.StatusBadRequest, "invalid_request"},
		{"duplicate include", "/repositories/7/tree?include=lastCommit&include=lastCommit", nil, http.StatusBadRequest, "invalid_request"},
		{"repository not found", "/repositories/7/tree", reposervice.ErrRepositoryNotFound, http.StatusNotFound, "repository_not_found"},
		{"ref not found", "/repositories/7/tree", reposervice.ErrRefNotFound, http.StatusNotFound, "ref_not_found"},
		{"path not found", "/repositories/7/tree", reposervice.ErrPathNotFound, http.StatusNotFound, "path_not_found"},
		{"invalid ref", "/repositories/7/tree", reposervice.ErrInvalidRef, http.StatusBadRequest, "invalid_request"},
		{"invalid path", "/repositories/7/tree", reposervice.ErrInvalidPath, http.StatusBadRequest, "invalid_request"},
		{"internal", "/repositories/7/tree", io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestRouter(&fakeManager{treeErr: tc.serviceErr}).ServeHTTP(
				rec,
				httptest.NewRequest(http.MethodGet, tc.target, nil),
			)
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
		})
	}
}

func TestGetDiff(t *testing.T) {
	headSHA := "1111222233334444555566667777888899990000"
	patch := "diff --git a/old.txt b/new.txt\n"
	svc := &fakeManager{diffResult: domain.RepositoryDiff{
		BaseSHA: testSHA,
		HeadSHA: headSHA,
		Files: []domain.DiffFile{
			{
				Status:     domain.DiffRenamed,
				OldPath:    "old.txt",
				NewPath:    "new.txt",
				OldBlobSHA: "2222333344445555666677778888999900001111",
				NewBlobSHA: "3333444455556666777788889999000011112222",
				OldMode:    "100644",
				NewMode:    "100755",
				Additions:  2,
				Deletions:  1,
				Patch:      &patch,
			},
			{
				Status:             domain.DiffModified,
				OldPath:            "image.png",
				NewPath:            "image.png",
				Binary:             true,
				PatchOmittedReason: domain.DiffPatchBinary,
			},
		},
	}}

	rec := httptest.NewRecorder()
	newTestRouter(svc).ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet,
		"/repositories/7/diff?base=main~1&head=main",
		nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if svc.diffBase != "main~1" || svc.diffHead != "main" {
		t.Errorf("Diff args = %q, %q", svc.diffBase, svc.diffHead)
	}

	var body struct {
		Data Diff `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.BaseSHA != testSHA || body.Data.HeadSHA != headSHA || len(body.Data.Files) != 2 {
		t.Fatalf("diff = %+v", body.Data)
	}
	if file := body.Data.Files[0]; file.Status != domain.DiffRenamed || file.OldPath != "old.txt" || file.NewPath != "new.txt" ||
		file.OldBlobSHA == "" || file.NewBlobSHA == "" || file.Additions == nil || *file.Additions != 2 ||
		file.Patch == nil || *file.Patch != patch {
		t.Errorf("renamed file = %+v", file)
	}
	if file := body.Data.Files[1]; !file.Binary || file.Additions != nil || file.Deletions != nil ||
		file.Patch != nil || file.PatchOmittedReason != "binary" {
		t.Errorf("binary file = %+v", file)
	}

	var required struct {
		Data struct {
			Truncated *bool `json:"truncated"`
			Files     []struct {
				Binary *bool           `json:"binary"`
				Patch  json.RawMessage `json:"patch"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &required); err != nil {
		t.Fatal(err)
	}
	if required.Data.Truncated == nil || *required.Data.Truncated {
		t.Errorf("truncated must be present and false: %s", rec.Body.String())
	}
	for i, file := range required.Data.Files {
		if file.Binary == nil {
			t.Errorf("files[%d].binary is missing: %s", i, rec.Body.String())
		}
	}
	if got := string(required.Data.Files[1].Patch); got != "null" {
		t.Errorf("binary patch = %s, want null", got)
	}
}

func TestGetDiffAcceptsZeroSHA(t *testing.T) {
	zeroSHA := strings.Repeat("0", 40)
	for _, tc := range []struct {
		name string
		base string
		head string
	}{
		{"empty base", zeroSHA, "main"},
		{"empty head", "main", zeroSHA},
		{"both empty", zeroSHA, zeroSHA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeManager{diffResult: domain.RepositoryDiff{Files: []domain.DiffFile{}}}
			rec := httptest.NewRecorder()
			newTestRouter(svc).ServeHTTP(rec, httptest.NewRequest(
				http.MethodGet,
				"/repositories/7/diff?base="+tc.base+"&head="+tc.head,
				nil,
			))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			if svc.diffBase != tc.base || svc.diffHead != tc.head {
				t.Errorf("Diff args = %q, %q", svc.diffBase, svc.diffHead)
			}
		})
	}
}

func TestGetDiffErrors(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"bad id", "/repositories/nope/diff?base=a&head=b", nil, http.StatusBadRequest, "invalid_request"},
		{"missing base", "/repositories/7/diff?head=main", nil, http.StatusBadRequest, "invalid_request"},
		{"missing head", "/repositories/7/diff?base=main~1", nil, http.StatusBadRequest, "invalid_request"},
		{"repository not found", "/repositories/7/diff?base=a&head=b", reposervice.ErrRepositoryNotFound, http.StatusNotFound, "repository_not_found"},
		{"ref not found", "/repositories/7/diff?base=a&head=b", reposervice.ErrRefNotFound, http.StatusNotFound, "ref_not_found"},
		{"invalid ref", "/repositories/7/diff?base=a&head=b", reposervice.ErrInvalidRef, http.StatusBadRequest, "invalid_request"},
		{"internal", "/repositories/7/diff?base=a&head=b", io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestRouter(&fakeManager{diffErr: tc.serviceErr}).ServeHTTP(
				rec,
				httptest.NewRequest(http.MethodGet, tc.target, nil),
			)
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
		})
	}
}

func TestGetCommit(t *testing.T) {
	authoredAt := time.Date(2026, 7, 30, 18, 40, 0, 0, time.UTC)
	committedAt := time.Date(2026, 7, 30, 18, 42, 0, 0, time.UTC)
	parentSHA := strings.Repeat("b", 40)
	svc := &fakeManager{commitDetails: domain.CommitDetails{
		SHA:         testSHA,
		Parents:     []string{parentSHA},
		Message:     "Update server configuration\n\nFull message.",
		Author:      domain.CommitIdentity{Name: "Alex Developer", Email: "alex@example.com"},
		AuthoredAt:  authoredAt,
		CommittedAt: committedAt,
	}}

	rec := httptest.NewRecorder()
	newTestRouter(svc).ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet,
		"/repositories/7/commits/"+testSHA,
		nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if svc.getCommitRepoID != 7 || svc.getCommitSHA != testSHA {
		t.Errorf("GetCommit args = %d, %q", svc.getCommitRepoID, svc.getCommitSHA)
	}

	var body struct {
		Data CommitDetails `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Data
	if got.SHA != testSHA || len(got.Parents) != 1 || got.Parents[0] != parentSHA ||
		got.Message != svc.commitDetails.Message || got.Author.Name != "Alex Developer" || got.Author.Email != "alex@example.com" ||
		!got.AuthoredAt.Equal(authoredAt) || !got.CommittedAt.Equal(committedAt) {
		t.Errorf("commit = %+v", got)
	}
}

func TestGetRootCommitParentsIsArray(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestRouter(&fakeManager{commitDetails: domain.CommitDetails{
		SHA:     testSHA,
		Parents: []string{},
	}}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repositories/7/commits/"+testSHA, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"parents":[]`) {
		t.Errorf("parents must be an array: %s", rec.Body.String())
	}
}

func TestGetCommitErrors(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"bad id", "/repositories/nope/commits/" + testSHA, nil, http.StatusBadRequest, "invalid_request"},
		{"invalid sha", "/repositories/7/commits/nope", reposervice.ErrInvalidCommitSHA, http.StatusBadRequest, "invalid_request"},
		{"repository not found", "/repositories/7/commits/" + testSHA, reposervice.ErrRepositoryNotFound, http.StatusNotFound, "repository_not_found"},
		{"commit not found", "/repositories/7/commits/" + testSHA, reposervice.ErrCommitNotFound, http.StatusNotFound, "commit_not_found"},
		{"internal", "/repositories/7/commits/" + testSHA, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestRouter(&fakeManager{getCommitErr: tc.serviceErr}).ServeHTTP(
				rec,
				httptest.NewRequest(http.MethodGet, tc.target, nil),
			)
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
		})
	}
}

func TestGetArchive(t *testing.T) {
	router := newTestRouter(&fakeManager{prepareReq: testArchiveRequest(), streamBody: "hello"})

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
	if got := rec.Header().Get("ETag"); got != archiveETag(testArchiveRequest()) {
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
	router := newTestRouter(&fakeManager{prepareReq: testArchiveRequest(), streamBody: "hello"})

	req := httptest.NewRequest(http.MethodGet, "/repositories/7/archive?ref=main", nil)
	req.Header.Set("If-None-Match", archiveETag(testArchiveRequest()))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("304 must have no body, got %d bytes", rec.Body.Len())
	}
}

func TestGetArchivePrefixParameter(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		want    string
		wantSet bool
	}{
		{"omitted", "/repositories/7/archive", "", false},
		{"custom", "/repositories/7/archive?prefix=release%2Fsource", "release/source", true},
		{"empty", "/repositories/7/archive?prefix=", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeManager{prepareReq: testArchiveRequest(), streamBody: "hello"}
			rec := httptest.NewRecorder()
			newTestRouter(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			if svc.prefixSet != tc.wantSet || svc.prefix != tc.want {
				t.Errorf("prefix = %q, set %v; want %q, set %v", svc.prefix, svc.prefixSet, tc.want, tc.wantSet)
			}
		})
	}
}

func TestArchiveETagVariesByPrefix(t *testing.T) {
	a := testArchiveRequest()
	b := testArchiveRequest()
	b.Prefix = "release/"
	if archiveETag(a) == archiveETag(b) {
		t.Fatal("archive ETags must vary by prefix")
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
		{"duplicate prefix", "/repositories/7/archive?prefix=a&prefix=b", nil, nil, http.StatusBadRequest, "invalid_request"},
		{"repo not found", "/repositories/7/archive", reposervice.ErrRepositoryNotFound, nil, http.StatusNotFound, "repository_not_found"},
		{"ref not found", "/repositories/7/archive?ref=nope", reposervice.ErrRefNotFound, nil, http.StatusNotFound, "ref_not_found"},
		{"invalid ref", "/repositories/7/archive?ref=--x", reposervice.ErrInvalidRef, nil, http.StatusBadRequest, "invalid_request"},
		{"bad format", "/repositories/7/archive?format=rar", reposervice.ErrUnsupportedFormat, nil, http.StatusBadRequest, "invalid_request"},
		{"bad prefix", "/repositories/7/archive?prefix=..", reposervice.ErrInvalidArchivePrefix, nil, http.StatusBadRequest, "invalid_request"},
		{"lfs disabled", "/repositories/7/archive?lfs=true", reposervice.ErrLFSNotEnabled, nil, http.StatusBadRequest, "invalid_request"},
		{"stream fails before first byte", "/repositories/7/archive", nil, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(&fakeManager{prepareReq: testArchiveRequest(), prepareErr: tc.prepareErr, streamErr: tc.streamErr})
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

func testFileRequest() domain.FileRequest {
	return domain.FileRequest{
		Repository: domain.Repository{ID: 7, RepositoryName: "myrepo"},
		BlobSHA:    "1111222233334444555566667777888899990000",
		Size:       6,
	}
}

func TestGetFile(t *testing.T) {
	router := newTestRouter(&fakeManager{fileInfo: domain.FileInfo{
		BlobSHA: "1111222233334444555566667777888899990000",
		Size:    2183912,
	}})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repositories/7/files/1111222233334444555566667777888899990000", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data File `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.BlobSHA != "1111222233334444555566667777888899990000" || body.Data.Size != 2183912 {
		t.Errorf("file = %+v", body.Data)
	}
}

func TestGetFileContent(t *testing.T) {
	router := newTestRouter(&fakeManager{fileReq: testFileRequest(), streamBody: "hello\n"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repositories/7/files/1111222233334444555566667777888899990000/content", nil))

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
	if got := rec.Header().Get("X-HeadlessGit-Commit"); got != "" {
		t.Errorf("X-HeadlessGit-Commit = %q", got)
	}
}

func TestGetFileContentNotModified(t *testing.T) {
	router := newTestRouter(&fakeManager{fileReq: testFileRequest(), streamBody: "hello\n"})

	req := httptest.NewRequest(http.MethodGet, "/repositories/7/files/1111222233334444555566667777888899990000/content", nil)
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

func TestCreateUpload(t *testing.T) {
	expiresAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	service := &fakeManager{uploadTarget: domain.UploadTarget{
		UploadID:  "upload-id",
		Href:      "https://git.test/uploads/upload-id?signature=signed",
		Header:    map[string]string{"Content-Type": "application/octet-stream"},
		ExpiresAt: expiresAt,
	}}
	rec := httptest.NewRecorder()
	newTestRouter(service).ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost,
		"/repositories/7/uploads",
		strings.NewReader(`{"userId":42,"size":1024}`),
	))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if service.uploadRepositoryID != 7 || service.uploadUserID != 42 || service.uploadSize != 1024 {
		t.Fatalf("arguments = repository %d, user %d, size %d", service.uploadRepositoryID, service.uploadUserID, service.uploadSize)
	}
	var body struct {
		Data UploadTarget `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.UploadURL != service.uploadTarget.Href || !body.Data.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("response = %+v", body.Data)
	}
}

func TestGetFileErrors(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		fileErr    error
		streamErr  error
		wantStatus int
		wantCode   string
	}{
		{"bad id", "/repositories/abc/files/" + strings.Repeat("a", 40), nil, nil, http.StatusBadRequest, "invalid_request"},
		{"repo not found", "/repositories/7/files/" + strings.Repeat("a", 40), reposervice.ErrRepositoryNotFound, nil, http.StatusNotFound, "repository_not_found"},
		{"unknown blob", "/repositories/7/files/" + strings.Repeat("a", 40), reposervice.ErrUnknownBlob, nil, http.StatusNotFound, "unknown_blob"},
		{"lfs object missing", "/repositories/7/files/" + strings.Repeat("a", 40) + "/content", reposervice.ErrLFSObjectNotFound, nil, http.StatusNotFound, "lfs_object_not_found"},
		{"not a file", "/repositories/7/files/" + strings.Repeat("a", 40), reposervice.ErrNotAFile, nil, http.StatusBadRequest, "invalid_request"},
		{"invalid sha", "/repositories/7/files/nope", reposervice.ErrInvalidBlobSHA, nil, http.StatusBadRequest, "invalid_request"},
		{"lfs unavailable", "/repositories/7/files/" + strings.Repeat("a", 40) + "/content", reposervice.ErrLFSNotEnabled, nil, http.StatusServiceUnavailable, "lfs_unavailable"},
		{"stream fails before first byte", "/repositories/7/files/" + strings.Repeat("a", 40) + "/content", nil, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(&fakeManager{fileReq: testFileRequest(), fileErr: tc.fileErr, streamErr: tc.streamErr})
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
		})
	}
}

func validCommitBody() string {
	return `{
		"branch": "main",
		"message": "update",
		"author": {"name": "api-user", "email": "api@test"},
		"expectedHeadSha": "` + strings.Repeat("a", 40) + `",
		"pusherId": 42,
		"operations": [
			{"op": "put", "path": "run.sh", "sha": "` + strings.Repeat("b", 40) + `", "executable": true},
			{"op": "delete", "path": "old.txt"}
		]
	}`
}

func TestCreateCommit(t *testing.T) {
	result := domain.CommitResult{Branch: "main", CommitSHA: testSHA, Before: strings.Repeat("a", 40)}
	fake := &fakeManager{commitResult: result}

	rec := httptest.NewRecorder()
	newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories/7/commits", strings.NewReader(validCommitBody())))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data Commit `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.CommitSHA != testSHA || body.Data.Before != result.Before || body.Data.Branch != "main" {
		t.Errorf("body = %+v", body.Data)
	}

	// the service must receive the fully mapped domain request
	req := fake.commitReq
	if req.Branch != "main" || req.Message != "update" || req.PusherID != 42 ||
		req.Author.Name != "api-user" || req.ExpectedHeadSHA != strings.Repeat("a", 40) {
		t.Errorf("service request = %+v", req)
	}
	if len(req.Operations) != 2 ||
		req.Operations[0].Delete || !req.Operations[0].Executable || req.Operations[0].SHA != strings.Repeat("b", 40) ||
		!req.Operations[1].Delete || req.Operations[1].Path != "old.txt" {
		t.Errorf("service operations = %+v", req.Operations)
	}
}

func TestCreateCommitMove(t *testing.T) {
	fake := &fakeManager{commitResult: domain.CommitResult{Branch: "main", CommitSHA: testSHA}}
	body := `{
		"branch":"main",
		"message":"reorganize",
		"author":{"name":"api-user","email":"api@test"},
		"operations":[{"op":"move","fromPath":"plugins","path":"server/plugins"}]
	}`
	rec := httptest.NewRecorder()
	newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories/7/commits", strings.NewReader(body)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if len(fake.commitReq.Operations) != 1 || fake.commitReq.Operations[0].MoveFrom != "plugins" ||
		fake.commitReq.Operations[0].Path != "server/plugins" {
		t.Errorf("service operations = %+v", fake.commitReq.Operations)
	}
}

func TestCreateCommitValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not json", "nope"},
		{"missing branch", `{"message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"delete","path":"a"}]}`},
		{"missing message", `{"branch":"main","author":{"name":"a","email":"e"},"operations":[{"op":"delete","path":"a"}]}`},
		{"missing author", `{"branch":"main","message":"x","operations":[{"op":"delete","path":"a"}]}`},
		{"no operations", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[]}`},
		{"bad op kind", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"copy","path":"a"}]}`},
		{"move without source", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"move","path":"a"}]}`},
		{"move with sha", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"move","fromPath":"a","path":"b","sha":"abc"}]}`},
		{"put with source", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"put","fromPath":"old","path":"a","sha":"abc"}]}`},
		{"put without object", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"put","path":"a"}]}`},
		{"delete with sha", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"delete","path":"a","sha":"abc"}]}`},
		{"legacy blobSha", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"put","path":"a","blobSha":"abc"}]}`},
		{"legacy lfs object", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"put","path":"a","lfs":{"oid":"def","size":1}}]}`},
		{"missing path", `{"branch":"main","message":"x","author":{"name":"a","email":"e"},"operations":[{"op":"delete"}]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeManager{}
			rec := httptest.NewRecorder()
			newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories/7/commits", strings.NewReader(tc.body)))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			if fake.commitCalled {
				t.Error("service must not be called on validation failure")
			}
		})
	}
}

func TestCreateCommitErrors(t *testing.T) {
	cases := []struct {
		name       string
		commitErr  error
		wantStatus int
		wantCode   string
	}{
		{"repo not found", reposervice.ErrRepositoryNotFound, http.StatusNotFound, "repository_not_found"},
		{"branch not found", reposervice.ErrRefNotFound, http.StatusNotFound, "ref_not_found"},
		{"delete target missing", reposervice.ErrPathNotFound, http.StatusNotFound, "path_not_found"},
		{"move destination exists", reposervice.ErrPathConflict, http.StatusConflict, "path_conflict"},
		{"head mismatch", reposervice.ErrHeadMismatch, http.StatusConflict, "head_mismatch"},
		{"unknown blob", reposervice.ErrUnknownBlob, http.StatusUnprocessableEntity, "unknown_blob"},
		{"nothing to commit", reposervice.ErrNothingToCommit, http.StatusUnprocessableEntity, "nothing_to_commit"},
		{"path blocked", reposervice.ErrPathBlocked, http.StatusUnprocessableEntity, "path_blocked"},
		{"delete target is a dir", reposervice.ErrNotAFile, http.StatusBadRequest, "invalid_request"},
		{"invalid branch", reposervice.ErrInvalidBranch, http.StatusBadRequest, "invalid_request"},
		{"invalid ops", reposervice.ErrInvalidCommitOps, http.StatusBadRequest, "invalid_request"},
		{"lfs not enabled", reposervice.ErrLFSNotEnabled, http.StatusBadRequest, "invalid_request"},
		{"internal", io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestRouter(&fakeManager{commitErr: tc.commitErr}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories/7/commits", strings.NewReader(validCommitBody())))

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
		})
	}
}

func TestCreateRepository(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		createErr  error
		wantStatus int
		wantCode   string
	}{
		{"created", `{"ownerId":3,"name":"demo","visibility":"private"}`, nil, http.StatusCreated, ""},
		{"duplicate", `{"ownerId":3,"name":"demo","visibility":"private"}`, reposervice.ErrRepositoryExists, http.StatusConflict, "repository_exists"},
		{"invalid name", `{"ownerId":3,"name":"..","visibility":"private"}`, reposervice.ErrInvalidRepositoryName, http.StatusBadRequest, "invalid_request"},
		{"internal", `{"ownerId":3,"name":"demo","visibility":"private"}`, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
		{"invalid body", `not json`, nil, http.StatusBadRequest, "invalid_request"},
		{"missing owner", `{"name":"demo","visibility":"private"}`, nil, http.StatusBadRequest, "invalid_request"},
		{"bad visibility", `{"ownerId":3,"name":"demo","visibility":"hidden"}`, nil, http.StatusBadRequest, "invalid_request"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeManager{
				createdRepo: domain.Repository{ID: 7, OwnerID: 3, RepositoryName: "demo", Visibility: domain.RepoVisibilityPrivate},
				createErr:   tc.createErr,
			}
			rec := httptest.NewRecorder()
			newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories", strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantCode != "" {
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
			}
		})
	}
}

func TestGetRepositoryByPath(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{"found", "/repositories/by-path/acme/api", nil, http.StatusOK, ""},
		{"numeric namespace and name", "/repositories/by-path/123/456", nil, http.StatusOK, ""},
		{"not found", "/repositories/by-path/acme/nope", reposervice.ErrRepositoryNotFound, http.StatusNotFound, "repository_not_found"},
		{"internal", "/repositories/by-path/acme/api", io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeManager{
				repoByPath:    domain.Repository{ID: 7, OwnerID: 3, RepositoryName: "api", Visibility: domain.RepoVisibilityPrivate},
				repoByPathErr: tc.svcErr,
			}
			rec := httptest.NewRecorder()
			newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantCode == "" {
				var body struct {
					Data Repository `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Data.ID != 7 || body.Data.Name != "api" {
					t.Errorf("body = %+v", body.Data)
				}
				return
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
		})
	}
}

func TestPathPolicies(t *testing.T) {
	policy := domain.PathPolicy{ID: 3, RepositoryID: 7, Pattern: "runtime", Kind: domain.PathPolicyBlock, Reason: "deploy state"}

	t.Run("add", func(t *testing.T) {
		fake := &fakeManager{policy: policy}
		rec := httptest.NewRecorder()
		newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories/7/path-policies",
			strings.NewReader(`{"pattern": "/runtime/", "reason": "deploy state"}`)))

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data PathPolicy `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Pattern != "runtime" || body.Data.Kind != "block" || body.Data.Reason != "deploy state" {
			t.Errorf("body = %+v", body.Data)
		}
		if fake.policyPattern != "/runtime/" || fake.policyReason != "deploy state" {
			t.Errorf("service got (%q, %q)", fake.policyPattern, fake.policyReason)
		}
	})

	t.Run("list", func(t *testing.T) {
		fake := &fakeManager{policyList: []domain.PathPolicy{policy}}
		rec := httptest.NewRecorder()
		newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repositories/7/path-policies", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data []PathPolicy `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Data) != 1 || body.Data[0].Pattern != "runtime" {
			t.Errorf("body = %+v", body.Data)
		}
	})

	t.Run("delete", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newTestRouter(&fakeManager{}).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/repositories/7/path-policies/3", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("errors", func(t *testing.T) {
		cases := []struct {
			name       string
			method     string
			target     string
			body       string
			policyErr  error
			wantStatus int
			wantCode   string
		}{
			{"missing pattern", http.MethodPost, "/repositories/7/path-policies", `{}`, nil, http.StatusBadRequest, "invalid_request"},
			{"invalid pattern", http.MethodPost, "/repositories/7/path-policies", `{"pattern":"a/../b"}`, reposervice.ErrInvalidPathPattern, http.StatusBadRequest, "invalid_request"},
			{"duplicate", http.MethodPost, "/repositories/7/path-policies", `{"pattern":"runtime"}`, reposervice.ErrPathPolicyExists, http.StatusConflict, "path_policy_exists"},
			{"repo not found", http.MethodGet, "/repositories/7/path-policies", "", reposervice.ErrRepositoryNotFound, http.StatusNotFound, "repository_not_found"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fake := &fakeManager{policyErr: tc.policyErr}
				rec := httptest.NewRecorder()
				var reqBody io.Reader
				if tc.body != "" {
					reqBody = strings.NewReader(tc.body)
				}
				newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, reqBody))

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
			})
		}
	})
}
