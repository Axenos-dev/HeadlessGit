package repositories

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) setVisibility(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req UpdateVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	repo, err := h.service.SetVisibility(r.Context(), id, domain.RepoVisibility(req.Visibility))
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrInvalidVisibility):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid visibility")
	case err != nil:
		h.logger.Error("failed to update repository visibility", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to update repository visibility")
	}

	return response.Data(w, http.StatusOK, newRepository(repo))
}
