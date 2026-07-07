package repositories

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	reposervice "github.com/Axenos-dev/HeadlessGit/internal/services/repositories"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) listPathPolicies(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	policies, err := h.service.ListPathPolicies(r.Context(), id)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case err != nil:
		h.logger.Error("failed to list path policies", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to list path policies")
	}

	out := make([]PathPolicy, len(policies))
	for i, p := range policies {
		out[i] = newPathPolicy(p)
	}
	return response.Data(w, http.StatusOK, out)
}

func (h *handlers) addPathPolicy(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req AddPathPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	policy, err := h.service.AddPathPolicy(r.Context(), id, req.Pattern, req.Reason)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case errors.Is(err, reposervice.ErrInvalidPathPattern):
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid path pattern")
	case errors.Is(err, reposervice.ErrPathPolicyExists):
		return response.NewError(http.StatusConflict, response.CodePathPolicyExists, "path policy already exists")
	case err != nil:
		h.logger.Error("failed to add path policy", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to add path policy")
	}

	return response.Data(w, http.StatusCreated, newPathPolicy(policy))
}

func (h *handlers) removePathPolicy(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}
	policyID, err := strconv.ParseInt(chi.URLParam(r, "policyID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid policy id")
	}

	err = h.service.RemovePathPolicy(r.Context(), id, policyID)
	switch {
	case errors.Is(err, reposervice.ErrRepositoryNotFound):
		return response.NewError(http.StatusNotFound, response.CodeRepositoryNotFound, "repository not found")
	case err != nil:
		h.logger.Error("failed to remove path policy", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to remove path policy")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
