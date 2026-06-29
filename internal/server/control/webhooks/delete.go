package webhooks

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) deleteWebhook(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}
	webhookID, err := strconv.ParseInt(chi.URLParam(r, "webhookID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid webhook id")
	}

	// scoped to the repo; deleting a non-existent hook is a no-op
	if err := h.webhooks.DeleteWebhook(r.Context(), webhookID, repoID); err != nil {
		h.logger.Error("failed to delete webhook", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to delete webhook")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
