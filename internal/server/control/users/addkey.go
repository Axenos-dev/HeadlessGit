package users

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) addSSHKey(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	var req AddSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	key, err := h.creds.AddSSHKey(r.Context(), userID, req.Title, req.PublicKey)
	if err != nil {
		return h.usersError(err, "failed to add ssh key")
	}

	return response.Data(w, http.StatusCreated, newSSHKeyResponse(key))
}
