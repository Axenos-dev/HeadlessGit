package users

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"go.uber.org/zap"
)

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) error {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	account, err := h.users.Create(r.Context(), domain.UserInfo{
		Username: req.Username,
		Kind:     domain.UserKind(req.Kind),
	})
	switch {
	case errors.Is(err, usersservice.ErrUserExists):
		return response.NewError(http.StatusConflict, response.CodeUserExists, "user already exists")
	case err != nil:
		h.logger.Error("failed to create user", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to create user")
	}

	return response.Data(w, http.StatusCreated, newUserResponse(account))
}
