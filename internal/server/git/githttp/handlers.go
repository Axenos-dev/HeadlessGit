package githttp

import (
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Handlers struct {
	logger   *zap.Logger
	repoRoot string
}

func NewHandlers(logger *zap.Logger, repoRoot string) *Handlers {
	return &Handlers{
		logger:   logger,
		repoRoot: repoRoot,
	}
}

func (h *Handlers) RegisterRoutes(githttp chi.Router) {
	// just hand request to git http-backend CGI script
	githttp.Handle("/*", h.backend())
}

func (h *Handlers) backend() http.Handler {
	// look for git path in system
	gitPath, _ := exec.LookPath("git")
	root, _ := filepath.Abs(h.repoRoot)

	return &cgi.Handler{
		Path: gitPath,
		Args: []string{"http-backend"},
		Dir:  root,
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
			"REMOTE_USER=anonymous", // no auth yet
			"PATH=" + os.Getenv("PATH"),
		},
	}
}
