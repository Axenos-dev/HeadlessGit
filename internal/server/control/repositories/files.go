package repositories

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) getFile(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	info, err := h.service.GetFile(r.Context(), id, chi.URLParam(r, "blobSHA"))
	if err != nil {
		return h.repositoriesError(err, "failed to read file metadata")
	}

	return response.Data(w, http.StatusOK, File{BlobSHA: info.BlobSHA, Size: info.Size})
}

func (h *handlers) getFileContent(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	req, err := h.service.PrepareFile(r.Context(), id, chi.URLParam(r, "blobSHA"))
	if err != nil {
		return h.repositoriesError(err, "failed to prepare file")
	}

	etag := fmt.Sprintf(`"%s"`, req.BlobSHA)
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(req.Size, 10))
	w.Header().Set("ETag", etag)

	cw := &countingWriter{w: w}
	if err := h.service.StreamFile(r.Context(), req, cw); err != nil {
		if cw.n == 0 {
			w.Header().Del("Content-Type")
			w.Header().Del("Content-Length")
			w.Header().Del("ETag")
			h.logger.Error("failed to stream file", zap.Int64("repository_id", id), zap.Error(err))
			return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to stream file")
		}

		h.logger.Error("file stream aborted mid-flight",
			zap.Int64("repository_id", id),
			zap.Int64("bytes_written", cw.n),
			zap.Error(err),
		)
	}
	return nil
}
