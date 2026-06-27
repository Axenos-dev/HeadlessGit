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

func (h *handlers) listTokens(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	tokens, err := h.creds.ListTokens(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to list tokens", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to list tokens")
	}

	return response.Data(w, http.StatusOK, newTokenResponses(tokens))
}

func (h *handlers) revokeToken(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}
	tokenID, err := strconv.ParseInt(chi.URLParam(r, "tokenID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid token id")
	}

	switch err := h.creds.RevokeToken(r.Context(), userID, tokenID); {
	case errors.Is(err, authservice.ErrTokenNotFound):
		return response.NewError(http.StatusNotFound, response.CodeTokenNotFound, "token not found")
	case err != nil:
		h.logger.Error("failed to revoke token", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to revoke token")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handlers) revokeAllTokens(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	if err := h.creds.RevokeAllTokens(r.Context(), userID); err != nil {
		h.logger.Error("failed to revoke tokens", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to revoke tokens")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
