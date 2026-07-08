package repositories

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"go.uber.org/zap"
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
	switch {
	case errors.Is(err, reposervice.ErrInvalidRepositoryName):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository name")
	case errors.Is(err, reposervice.ErrRepositoryExists):
		return response.NewError(http.StatusConflict, response.CodeRepositoryExists, "repository already exists")
	case err != nil:
		h.logger.Error("failed to create repository", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to create repository")
	}

	return response.Data(w, http.StatusCreated, newRepository(repo))
}
