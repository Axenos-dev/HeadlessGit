package users

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listTokens(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	tokens, err := h.creds.ListTokens(r.Context(), userID)
	if err != nil {
		return h.usersError(err, "failed to list tokens")
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

	if err := h.creds.RevokeToken(r.Context(), userID, tokenID); err != nil {
		return h.usersError(err, "failed to revoke token")
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
		return h.usersError(err, "failed to revoke tokens")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
