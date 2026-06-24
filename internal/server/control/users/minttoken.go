package users

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) mintToken(w http.ResponseWriter, r *http.Request) error {
	userID, err := strconv.ParseInt(chi.URLParam(r, "userID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
	}

	var req MintTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	// no expiry for now
	raw, token, err := h.creds.MintToken(r.Context(), userID, req.Title, nil)
	if err != nil {
		h.logger.Error("failed to mint token", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to mint token")
	}

	return response.Data(w, http.StatusCreated, newMintTokenResponse(raw, token))
}
