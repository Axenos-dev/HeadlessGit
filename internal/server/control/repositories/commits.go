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
)

func (h *handlers) getCommit(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	commit, err := h.service.GetCommit(r.Context(), id, chi.URLParam(r, "sha"))
	if err != nil {
		return h.repositoriesError(err, "failed to get commit")
	}

	return response.Data(w, http.StatusOK, newCommitDetails(commit))
}

func (h *handlers) createCommit(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req CreateCommitRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	ops := make([]domain.CommitFileOp, len(req.Operations))
	for i, op := range req.Operations {
		ops[i] = domain.CommitFileOp{
			Delete:     op.Op == "delete",
			MoveFrom:   op.FromPath,
			Path:       op.Path,
			SHA:        op.SHA,
			Executable: op.Executable,
		}
	}

	result, err := h.service.Commit(r.Context(), id, domain.CommitRequest{
		Branch:          req.Branch,
		Message:         req.Message,
		Author:          domain.CommitIdentity{Name: req.Author.Name, Email: req.Author.Email},
		ExpectedHeadSHA: req.ExpectedHeadSHA,
		PusherID:        req.PusherID,
		Operations:      ops,
	})
	// a blob referenced from the body is a bad reference, not a missing resource
	if errors.Is(err, reposervice.ErrUnknownBlob) {
		return response.NewError(http.StatusUnprocessableEntity, response.CodeUnknownBlob, "referenced blob not found, upload it first")
	}
	if err != nil {
		return h.repositoriesError(err, "failed to create commit")
	}

	return response.Data(w, http.StatusCreated, newCommit(result))
}
