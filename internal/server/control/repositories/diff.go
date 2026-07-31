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
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrRefNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRefNotFound, "ref not found")
	case errors.Is(err, reposervice.ErrInvalidRef):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid ref")
	case err != nil:
		h.logger.Error("failed to diff repository", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to diff repository")
	}

	return response.Data(w, http.StatusOK, newDiff(diff))
}
