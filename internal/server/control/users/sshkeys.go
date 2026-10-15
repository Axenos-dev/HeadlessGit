package users

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listSSHKeys(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	keys, err := h.creds.ListSSHKeys(r.Context(), userID)
	if err != nil {
		return h.usersError(err, "failed to list ssh keys")
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

	if err := h.creds.RemoveSSHKeyByID(r.Context(), userID, keyID); err != nil {
		return h.usersError(err, "failed to delete ssh key")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
