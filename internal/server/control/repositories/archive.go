package repositories

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) getArchive(w http.ResponseWriter, r *http.Request) error {
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

	req, err := h.service.PrepareArchive(r.Context(), id, q.Get("ref"), q.Get("format"), includeLFS)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrRefNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRefNotFound, "ref not found")
	case errors.Is(err, reposervice.ErrInvalidRef):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid ref")
	case errors.Is(err, reposervice.ErrUnsupportedFormat):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "unsupported archive format")
	case errors.Is(err, reposervice.ErrLFSNotEnabled):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "lfs is not enabled")
	case err != nil:
		h.logger.Error("failed to prepare archive", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to prepare archive")
	}

	etag := archiveETag(req)
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	contentType := "application/zip"
	if req.Format == domain.ArchiveFormatTarGz {
		contentType = "application/gzip"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", req.Filename()))
	w.Header().Set("ETag", etag)
	w.Header().Set("X-HeadlessGit-Commit", req.CommitSHA)

	cw := &countingWriter{w: w}
	if err := h.service.StreamArchive(r.Context(), req, cw); err != nil {
		// nothing sent yet,
		if cw.n == 0 {
			// undo the archive headers
			w.Header().Del("Content-Type")
			w.Header().Del("Content-Disposition")
			w.Header().Del("ETag")
			w.Header().Del("X-HeadlessGit-Commit")
			h.logger.Error("failed to stream archive", zap.Int64("repository_id", id), zap.Error(err))
			// and return normal error
			return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to stream archive")
		}

		// otherwise just log, the bytes were already streamed
		h.logger.Error("archive stream aborted mid-flight",
			zap.Int64("repository_id", id),
			zap.Int64("bytes_written", cw.n),
			zap.Error(err),
		)
	}
	return nil
}

func archiveETag(req domain.ArchiveRequest) string {
	variant := string(req.Format)
	if req.IncludeLFS {
		variant += "-lfs"
	}
	return fmt.Sprintf(`W/"%s-%s"`, req.CommitSHA, variant)
}

// just to keep track how much bytes were streamed
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
