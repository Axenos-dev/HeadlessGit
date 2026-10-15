package repositories

import (
	"encoding/json"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
)

func (h *handlers) createRepository(w http.ResponseWriter, r *http.Request) error {
	var req CreateRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	repo, err := h.service.Create(r.Context(), req.OwnerID, domain.RepositoryInfo{
		RepositoryName: req.Name,
		Visibility:     domain.RepoVisibility(req.Visibility),
	})
	if err != nil {
		return h.repositoriesError(err, "failed to create repository")
	}

	return response.Data(w, http.StatusCreated, newRepository(repo))
}
