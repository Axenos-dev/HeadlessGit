package repositories

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) getBlob(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	q := r.URL.Query()
	includeLFS := false
	if raw := q.Get("lfs"); raw != "" {
		includeLFS, err = strconv.ParseBool(raw)
		if err != nil {
			return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "lfs must be a boolean")
		}
	}

	req, err := h.service.PrepareBlob(r.Context(), id, q.Get("ref"), q.Get("path"), includeLFS)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrRefNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRefNotFound, "ref not found")
	case errors.Is(err, reposervice.ErrPathNotFound):
		return response.NewError(http.StatusNotFound, response.CodePathNotFound, "path not found")
	case errors.Is(err, reposervice.ErrLFSObjectNotFound):
		return response.NewError(http.StatusNotFound, response.CodeLFSObjectNotFound, "lfs object not found")
	case errors.Is(err, reposervice.ErrNotAFile):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "path is not a file, use the contents endpoint")
	case errors.Is(err, reposervice.ErrInvalidRef):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid ref")
	case errors.Is(err, reposervice.ErrInvalidPath):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid path")
	case errors.Is(err, reposervice.ErrLFSNotEnabled):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "lfs is not enabled")
	case err != nil:
		h.logger.Error("failed to prepare blob", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to prepare blob")
	}

	etag := blobETag(req)
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(req.Size, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(req.Path)))
	w.Header().Set("ETag", etag)
	w.Header().Set("X-HeadlessGit-Commit", req.CommitSHA)

	cw := &countingWriter{w: w}
	if err := h.service.StreamBlob(r.Context(), req, cw); err != nil {
		// nothing sent yet
		if cw.n == 0 {
			// undo the blob headers
			w.Header().Del("Content-Type")
			w.Header().Del("Content-Length")
			w.Header().Del("Content-Disposition")
			w.Header().Del("ETag")
			w.Header().Del("X-HeadlessGit-Commit")
			h.logger.Error("failed to stream blob", zap.Int64("repository_id", id), zap.Error(err))
			// and return normal error
			return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to stream blob")
		}

		// otherwise just log, the bytes were already streamed
		h.logger.Error("blob stream aborted mid-flight",
			zap.Int64("repository_id", id),
			zap.Int64("bytes_written", cw.n),
			zap.Error(err),
		)
	}
	return nil
}

func blobETag(req domain.BlobRequest) string {
	if req.LFSOID != "" {
		return fmt.Sprintf(`"%s-lfs"`, req.BlobSHA)
	}
	return fmt.Sprintf(`"%s"`, req.BlobSHA)
}
