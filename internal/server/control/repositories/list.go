package repositories

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listUserRepositories(w http.ResponseWriter, r *http.Request) error {
	ownerID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	repos, err := h.service.ListByOwner(r.Context(), ownerID)
	if err != nil {
		return h.repositoriesError(err, "failed to list user repositories")
	}

	return response.Data(w, http.StatusOK, newRepositories(repos))
}
