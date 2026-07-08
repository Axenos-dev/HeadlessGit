package repositories

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) getRepository(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	repo, err := h.service.Get(r.Context(), id)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case err != nil:
		h.logger.Error("failed to get repository", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to get repository")
	}

	return response.Data(w, http.StatusOK, newRepository(repo))
}

func (h *handlers) getRepositoryByPath(w http.ResponseWriter, r *http.Request) error {
	namespace, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if namespace == "" || name == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "namespace and name are required")
	}

	repo, err := h.service.GetRepositoryByPath(r.Context(), namespace, name)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case err != nil:
		h.logger.Error("failed to get repository by path", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to get repository")
	}

	return response.Data(w, http.StatusOK, newRepository(repo))
}
