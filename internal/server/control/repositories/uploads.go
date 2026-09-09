package repositories

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) createUpload(w http.ResponseWriter, r *http.Request) error {
	repositoryID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req CreateUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	target, err := h.service.CreateUpload(r.Context(), repositoryID, req.UserID, req.Size)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrLFSNotEnabled), errors.Is(err, reposervice.ErrUploadUnavailable):
		return response.NewError(http.StatusServiceUnavailable, response.CodeLFSUnavailable, "lfs uploads are unavailable")
	case err != nil:
		return err
	}

	return response.Data(w, http.StatusCreated, newUploadTarget(target))
}
