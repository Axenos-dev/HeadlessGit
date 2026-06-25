package lfs

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"go.uber.org/zap"
)

func (h *Handlers) handleVerify(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r)
	if !ok {
		return
	}

	account := middleware.AccountFromContext(r.Context())
	if err := h.authz.Authorize(r.Context(), account, repo, domain.RoleWrite); err != nil {
		h.writeAuthError(w, account)
		return
	}

	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.service.Verify(r.Context(), repo, req.OID, req.Size)

	switch {
	case errors.Is(err, lfsservice.ErrInvalidOID):
		h.writeError(w, http.StatusUnprocessableEntity, "invalid object id")
		return

	case errors.Is(err, lfsservice.ErrObjectNotFound):
		h.writeError(w, http.StatusNotFound, "object not found")
		return

	case err != nil:
		h.logger.Error("lfs verify failed", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusOK)
}
