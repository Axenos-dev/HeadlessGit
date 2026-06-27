package permissions

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) revokePermission(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	// idempotent: removing a non-existent grant is a no-op
	if err := h.perms.Revoke(r.Context(), userID, repoID); err != nil {
		h.logger.Error("failed to revoke permission", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to revoke permission")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
