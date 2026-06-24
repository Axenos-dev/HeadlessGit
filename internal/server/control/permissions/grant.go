package permissions

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) grantPermission(w http.ResponseWriter, r *http.Request) error {
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req GrantPermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	err = h.perms.Grant(r.Context(), req.UserID, repoID, domain.Role(req.Role))
	switch {
	case errors.Is(err, permsservice.ErrInvalidRole):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid role")
	case err != nil:
		h.logger.Error("failed to grant permission", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to grant permission")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
