package repositories

import (
	"archive/zip"
	"context"
	"io"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
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

// newTestRouter mounts the handlers the same way the control server does
func newTestRouter(svc RepositoryManager) http.Handler {
	r := chi.NewRouter()
	NewHandlers(zap.NewNop(), svc).RegisterRoutes(r)
	return r
}
