package lfs

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	lfsservice "github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *Handlers) handleUploadOptions(w http.ResponseWriter, _ *http.Request) {
	setUploadCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) handleDirectUpload(w http.ResponseWriter, r *http.Request) {
	setUploadCORS(w)

	auth, err := parseUploadAuthorization(r)
	if err != nil {
		response.WriteError(w, h.logger, response.NewError(http.StatusUnauthorized, response.CodeUnauthorized, "invalid upload url"))
		return
	}
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		response.WriteError(w, h.logger, response.NewError(http.StatusUnsupportedMediaType, response.CodeInvalidRequest, "content type must be application/octet-stream"))
		return
	}
	if r.ContentLength != auth.Size {
		response.WriteError(w, h.logger, response.NewError(http.StatusUnprocessableEntity, response.CodeInvalidRequest, "content length does not match upload size"))
		return
	}

	object, err := h.service.Upload(r.Context(), auth, r.Body)
	switch {
	case errors.Is(err, lfsservice.ErrInvalidUpload):
		response.WriteError(w, h.logger, response.NewError(http.StatusUnauthorized, response.CodeUnauthorized, "invalid upload url"))
		return
	case errors.Is(err, lfsservice.ErrUploadExpired):
		response.WriteError(w, h.logger, response.NewError(http.StatusGone, response.CodeInvalidRequest, "upload url expired"))
		return
	case errors.Is(err, lfsservice.ErrObjectMismatch):
		response.WriteError(w, h.logger, response.NewError(http.StatusUnprocessableEntity, response.CodeInvalidRequest, "uploaded content size does not match"))
		return
	case err != nil:
		h.logger.Error("direct lfs upload failed", zap.Error(err))
		response.WriteError(w, h.logger, response.NewError(http.StatusInternalServerError, response.CodeInternalError, "upload failed"))
		return
	}

	if err := response.Data(w, http.StatusCreated, uploadedObject{OID: object.OID, Size: object.Size}); err != nil {
		h.logger.Warn("failed to encode upload response", zap.Error(err))
	}
}

func parseUploadAuthorization(r *http.Request) (domain.LFSUploadAuthorization, error) {
	query := r.URL.Query()
	repositoryID, err := strconv.ParseInt(query.Get("repositoryId"), 10, 64)
	if err != nil {
		return domain.LFSUploadAuthorization{}, err
	}
	userID, err := strconv.ParseInt(query.Get("userId"), 10, 64)
	if err != nil {
		return domain.LFSUploadAuthorization{}, err
	}
	size, err := strconv.ParseInt(query.Get("size"), 10, 64)
	if err != nil {
		return domain.LFSUploadAuthorization{}, err
	}
	expires, err := strconv.ParseInt(query.Get("expires"), 10, 64)
	if err != nil {
		return domain.LFSUploadAuthorization{}, err
	}

	return domain.LFSUploadAuthorization{
		UploadID:     chi.URLParam(r, "uploadID"),
		RepositoryID: repositoryID,
		UserID:       userID,
		Size:         size,
		ExpiresAt:    time.Unix(expires, 0).UTC(),
		Signature:    query.Get("signature"),
	}, nil
}

func setUploadCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", http.MethodPut)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
