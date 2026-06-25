package lfs

import (
	"errors"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *Handlers) handleUpload(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}

	account := middleware.AccountFromContext(r.Context())
	if err := h.authz.Authorize(r.Context(), account, repo, domain.RoleWrite); err != nil {
		h.writeAuthError(w, account)
		return
	}

	oid := chi.URLParam(r, "oid")
	err := h.service.PutObject(r.Context(), repo, oid, r.ContentLength, r.Body)

	switch {
	case errors.Is(err, lfsservice.ErrInvalidOID):
		h.writeError(w, http.StatusUnprocessableEntity, "invalid object id")
		return

	case errors.Is(err, lfsservice.ErrObjectMismatch):
		h.writeError(w, http.StatusUnprocessableEntity, "object content does not match oid/size")
		return

	case err != nil:
		h.logger.Error("lfs upload failed", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusOK)
}
