package repositories

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) getDiff(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	base, head := r.URL.Query().Get("base"), r.URL.Query().Get("head")
	if base == "" || head == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "base and head are required")
	}

	diff, err := h.service.Diff(r.Context(), id, base, head)
	if err != nil {
		return h.repositoriesError(err, "failed to diff repository")
	}

	return response.Data(w, http.StatusOK, newDiff(diff))
}
