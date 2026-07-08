package users

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	account, err := h.users.Get(r.Context(), userID)
	switch {
	case errors.Is(err, usersservice.ErrUserNotFound):
		return response.NewError(http.StatusNotFound, response.CodeUserNotFound, "user not found")
	case err != nil:
		h.logger.Error("failed to get user", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to get user")
	}

	return response.Data(w, http.StatusOK, newUserResponse(account))
}

func (h *handlers) getUserByUsername(w http.ResponseWriter, r *http.Request) error {
	username := chi.URLParam(r, "username")
	if username == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "username is required")
	}

	account, err := h.users.GetByUsername(r.Context(), username)
	switch {
	case errors.Is(err, usersservice.ErrUserNotFound):
		return response.NewError(http.StatusNotFound, response.CodeUserNotFound, "user not found")
	case err != nil:
		h.logger.Error("failed to get user by username", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to get user")
	}

	return response.Data(w, http.StatusOK, newUserResponse(account))
}
