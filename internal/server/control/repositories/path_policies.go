package repositories

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listPathPolicies(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	policies, err := h.service.ListPathPolicies(r.Context(), id)
	if err != nil {
		return h.repositoriesError(err, "failed to list path policies")
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
	if err != nil {
		return h.repositoriesError(err, "failed to add path policy")
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

	if err := h.service.RemovePathPolicy(r.Context(), id, policyID); err != nil {
		return h.repositoriesError(err, "failed to remove path policy")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
