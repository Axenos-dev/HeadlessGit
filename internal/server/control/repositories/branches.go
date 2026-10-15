package repositories

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) listBranches(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	listing, err := h.service.ListBranches(r.Context(), id)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case err != nil:
		h.logger.Error("failed to list branches", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to list branches")
	}

	return response.Data(w, http.StatusOK, newBranchList(listing))
}

func (h *handlers) createBranch(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req CreateBranchRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	branch, err := h.service.CreateBranch(r.Context(), id, domain.CreateBranchRequest{
		Name:     req.Name,
		SHA:      req.SHA,
		PusherID: req.PusherID,
	})
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrCommitNotFound):
		return response.NewError(http.StatusNotFound, response.CodeCommitNotFound, "commit not found")
	case errors.Is(err, reposervice.ErrBranchExists):
		return response.NewError(http.StatusConflict, response.CodeBranchExists, "branch already exists or its name conflicts with an existing branch")
	case errors.Is(err, reposervice.ErrInvalidBranch):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid branch name")
	case errors.Is(err, reposervice.ErrInvalidCommitSHA):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid commit sha")
	case err != nil:
		h.logger.Error("failed to create branch", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to create branch")
	}

	return response.Data(w, http.StatusCreated, newBranch(branch))
}

func (h *handlers) deleteBranch(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	name, err := branchName(r)
	if err != nil || name == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid branch name")
	}

	query := r.URL.Query()
	var pusherID int64
	if raw := query.Get("pusherId"); raw != "" {
		pusherID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid pusherId")
		}
	}

	err = h.service.DeleteBranch(r.Context(), id, domain.DeleteBranchRequest{
		Name:        name,
		ExpectedSHA: query.Get("expectedSha"),
		PusherID:    pusherID,
	})
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrRefNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRefNotFound, "branch not found")
	case errors.Is(err, reposervice.ErrHeadMismatch):
		return response.NewError(http.StatusConflict, response.CodeHeadMismatch, "branch head does not match expectedSha")
	case errors.Is(err, reposervice.ErrDefaultBranch):
		return response.NewError(http.StatusUnprocessableEntity, response.CodeDefaultBranch, "the default branch cannot be deleted")
	case errors.Is(err, reposervice.ErrInvalidBranch):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid branch name")
	case errors.Is(err, reposervice.ErrInvalidCommitSHA):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid expectedSha")
	case err != nil:
		h.logger.Error("failed to delete branch", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to delete branch")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// reads the branch name from the catch-all segment. chi matches against the
// escaped path whenever the client escaped something (feature%2Fx), so only
// that form needs decoding; a decoded name may legitimately contain a '%'
func branchName(r *http.Request) (string, error) {
	name := chi.URLParam(r, "*")
	if r.URL.RawPath == "" {
		return name, nil
	}
	return url.PathUnescape(name)
}
