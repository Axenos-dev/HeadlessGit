package repositories

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) deleteRepository(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	if err := h.service.Delete(r.Context(), id); err != nil {
		return h.repositoriesError(err, "failed to delete repository")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
