package users

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	account, err := h.users.Get(r.Context(), userID)
	if err != nil {
		return h.usersError(err, "failed to get user")
	}

	return response.Data(w, http.StatusOK, newUserResponse(account))
}

func (h *handlers) getUserByUsername(w http.ResponseWriter, r *http.Request) error {
	username := chi.URLParam(r, "username")
	if username == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "username is required")
	}

	account, err := h.users.GetByUsername(r.Context(), username)
	if err != nil {
		return h.usersError(err, "failed to get user by username")
	}

	return response.Data(w, http.StatusOK, newUserResponse(account))
}
