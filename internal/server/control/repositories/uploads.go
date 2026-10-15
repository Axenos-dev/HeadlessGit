package repositories

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
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
	if err != nil {
		return h.repositoriesError(err, "failed to create upload")
	}

	return response.Data(w, http.StatusCreated, newUploadTarget(target))
}
