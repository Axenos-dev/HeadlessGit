package repositories

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
)

func (h *handlers) listBranches(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	listing, err := h.service.ListBranches(r.Context(), id)
	if err != nil {
		return h.repositoriesError(err, "failed to list branches")
	}

	return response.Data(w, http.StatusOK, newBranchList(listing))
}

func (h *handlers) createBranch(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	var req CreateBranchRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid request body")
	}
	if err := req.Validate(); err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	}

	branch, err := h.service.CreateBranch(r.Context(), id, domain.CreateBranchRequest{
		Name:     req.Name,
		SHA:      req.SHA,
		PusherID: req.PusherID,
	})
	if err != nil {
		return h.repositoriesError(err, "failed to create branch")
	}

	return response.Data(w, http.StatusCreated, newBranch(branch))
}

func (h *handlers) deleteBranch(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	name, err := branchName(r)
	if err != nil || name == "" {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid branch name")
	}

	query := r.URL.Query()
	var pusherID int64
	if raw := query.Get("pusherId"); raw != "" {
		pusherID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid pusherId")
		}
	}

	err = h.service.DeleteBranch(r.Context(), id, domain.DeleteBranchRequest{
		Name:        name,
		ExpectedSHA: query.Get("expectedSha"),
		PusherID:    pusherID,
	})
	if err != nil {
		return h.repositoriesError(err, "failed to delete branch")
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// reads the branch name from the catch-all segment. chi matches against the
// escaped path whenever the client escaped something (feature%2Fx), so only
// that form needs decoding; a decoded name may legitimately contain a '%'
func branchName(r *http.Request) (string, error) {
	name := chi.URLParam(r, "*")
	if r.URL.RawPath == "" {
		return name, nil
	}
	return url.PathUnescape(name)
}
