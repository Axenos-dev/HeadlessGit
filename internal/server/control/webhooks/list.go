package webhooks

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	webhooks, err := h.webhooks.ListWebhooks(r.Context(), repoID)
	if err != nil {
		return h.webhooksError(err, "failed to list webhooks")
	}

	return response.Data(w, http.StatusOK, newWebhookListItems(webhooks))
}
