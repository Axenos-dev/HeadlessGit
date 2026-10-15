package repositories

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) getRepository(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	repo, err := h.service.Get(r.Context(), id)
	if err != nil {
		return h.repositoriesError(err, "failed to get repository")
	}

	return response.Data(w, http.StatusOK, newRepository(repo))
}

func (h *handlers) getRepositoryByPath(w http.ResponseWriter, r *http.Request) error {
	namespace, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if namespace == "" || name == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "namespace and name are required")
	}

	repo, err := h.service.GetRepositoryByPath(r.Context(), namespace, name)
	if err != nil {
		return h.repositoriesError(err, "failed to get repository by path")
	}

	return response.Data(w, http.StatusOK, newRepository(repo))
}
