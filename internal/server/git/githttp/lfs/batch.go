package lfs

import (
	"encoding/json"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/git/githttp/middleware"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *Handlers) handleBatch(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, "lfs-batch")
	if !ok {
		return
	}

	var req batchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	op := domain.LFSOperation(req.Operation)
	var required domain.Role
	switch op {
	// on upload we require write perms
	case domain.LFSOperationUpload:
		required = domain.RoleWrite
	// on download we require read perms
	case domain.LFSOperationDownload:
		required = domain.RoleRead
	default:
		h.writeError(w, http.StatusUnprocessableEntity, "unsupported operation")
		return
	}

	account := middleware.AccountFromContext(r.Context())
	if err := h.authz.Authorize(r.Context(), account, repo, required); err != nil {
		h.writeAuthError(w, account)
		return
	}

	var uploaderID int64
	if account != nil {
		uploaderID = account.UserID
	}

	pointers := make([]domain.LFSPointer, len(req.Objects))
	for i, o := range req.Objects {
		pointers[i] = domain.LFSPointer{OID: o.OID, Size: o.Size}
	}

	results, err := h.service.Batch(r.Context(), repo, chi.URLParam(r, "namespace"), op, uploaderID, pointers)
	if err != nil {
		h.logger.Error("lfs batch failed", zap.Error(err))
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.writeJSON(w, http.StatusOK, batchResponse{Transfer: "basic", Objects: toObjectsJSON(results)})
}
