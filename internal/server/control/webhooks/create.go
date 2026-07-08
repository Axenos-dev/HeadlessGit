package webhooks

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	webhookservice "github.com/Axenos-dev/HeadlessGit/internal/services/webhooks"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) createWebhook(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	webhook, err := h.webhooks.RegisterWebhook(r.Context(), repoID, req.URL)
	switch {
	case errors.Is(err, webhookservice.ErrWebhookExists):
		return response.NewError(http.StatusConflict, response.CodeWebhookExists, "webhook already exists")
	case err != nil:
		h.logger.Error("failed to register webhook", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to register webhook")
	}

	return response.Data(w, http.StatusCreated, newWebhookResponse(webhook))
}
