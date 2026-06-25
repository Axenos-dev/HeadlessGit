package lfs

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *Handlers) handleDownload(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}

	account := middleware.AccountFromContext(r.Context())
	if err := h.authz.Authorize(r.Context(), account, repo, domain.RoleRead); err != nil {
		h.writeAuthError(w, account)
		return
	}

	oid := chi.URLParam(r, "oid")
	rc, size, err := h.service.GetObject(r.Context(), repo, oid)
	switch {
	case errors.Is(err, lfsservice.ErrInvalidOID):
		h.writeError(w, http.StatusUnprocessableEntity, "invalid object id")
		return

	case errors.Is(err, lfsservice.ErrObjectNotFound):
		h.writeError(w, http.StatusNotFound, "object not found")
		return

	case err != nil:
		h.logger.Error("lfs download failed", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if _, err := io.Copy(w, rc); err != nil {
		h.logger.Warn("lfs download stream interrupted", zap.Error(err))
	}
}
