package webhooks

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	webhooks, err := h.webhooks.ListWebhooks(r.Context(), repoID)
	if err != nil {
		h.logger.Error("failed to list webhooks", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to list webhooks")
	}

	return response.Data(w, http.StatusOK, newWebhookListItems(webhooks))
}
