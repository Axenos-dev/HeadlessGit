package users

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	authservice "github.com/Axenos-dev/HeadlessGit/internal/services/auth"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) listSSHKeys(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	keys, err := h.creds.ListSSHKeys(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to list ssh keys", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to list ssh keys")
	}

	return response.Data(w, http.StatusOK, newSSHKeyResponses(keys))
}

func (h *handlers) deleteSSHKey(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}
	keyID, err := strconv.ParseInt(chi.URLParam(r, "keyID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid key id")
	}

	switch err := h.creds.RemoveSSHKeyByID(r.Context(), userID, keyID); {
	case errors.Is(err, authservice.ErrSSHKeyNotFound):
		return response.NewError(http.StatusNotFound, response.CodeSSHKeyNotFound, "ssh key not found")
	case err != nil:
		h.logger.Error("failed to delete ssh key", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to delete ssh key")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
