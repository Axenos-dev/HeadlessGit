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

func (h *handlers) getCommit(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	commit, err := h.service.GetCommit(r.Context(), id, chi.URLParam(r, "sha"))
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrCommitNotFound):
		return response.NewError(http.StatusNotFound, response.CodeCommitNotFound, "commit not found")
	case errors.Is(err, reposervice.ErrInvalidCommitSHA):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid commit sha")
	case err != nil:
		h.logger.Error("failed to get commit", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to get commit")
	}

	return response.Data(w, http.StatusOK, newCommitDetails(commit))
}

func (h *handlers) createCommit(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req CreateCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
			BlobSHA:    op.BlobSHA,
			Executable: op.Executable,
		}

		if op.Lfs != nil {
			ops[i].Lfs = &domain.CommitFileLfsObject{
				OID:  op.Lfs.OID,
				Size: op.Lfs.Size,
			}
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
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrRefNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRefNotFound, "branch not found; pass the all-zero expectedHeadSha to create it")
	case errors.Is(err, reposervice.ErrPathNotFound):
		return response.NewError(http.StatusNotFound, response.CodePathNotFound, "delete target or move source not found")
	case errors.Is(err, reposervice.ErrPathConflict):
		return response.NewError(http.StatusConflict, response.CodePathConflict, "move destination already exists")
	case errors.Is(err, reposervice.ErrHeadMismatch):
		return response.NewError(http.StatusConflict, response.CodeHeadMismatch, "branch head does not match expectedHeadSha")
	case errors.Is(err, reposervice.ErrUnknownBlob):
		return response.NewError(http.StatusUnprocessableEntity, response.CodeUnknownBlob, "referenced blob not found, upload it first")
	case errors.Is(err, reposervice.ErrLFSObjectNotFound):
		return response.NewError(http.StatusUnprocessableEntity, response.CodeLFSObjectNotFound, "referenced lfs object not found or not verified, upload it first")
	case errors.Is(err, reposervice.ErrNothingToCommit):
		return response.NewError(http.StatusUnprocessableEntity, response.CodeNothingToCommit, "operations produce no change")
	case errors.Is(err, reposervice.ErrPathBlocked):
		return response.NewError(http.StatusUnprocessableEntity, response.CodePathBlocked, err.Error())
	case errors.Is(err, reposervice.ErrNotAFile):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "delete target is not a file")
	case errors.Is(err, reposervice.ErrInvalidBranch):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid branch name")
	case errors.Is(err, reposervice.ErrInvalidCommitOps):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	case errors.Is(err, reposervice.ErrLFSNotEnabled):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "path is lfs-tracked but lfs is not enabled")
	case err != nil:
		h.logger.Error("failed to create commit", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to create commit")
	}

	return response.Data(w, http.StatusCreated, newCommit(result))
}
