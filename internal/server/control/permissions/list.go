package permissions

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listPermissions(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	perms, err := h.perms.List(r.Context(), repoID)
	if err != nil {
		return h.permissionsError(err, "failed to list permissions")
	}

	return response.Data(w, http.StatusOK, newPermissions(perms))
}
