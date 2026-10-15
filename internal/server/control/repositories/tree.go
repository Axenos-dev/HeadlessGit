package repositories

import (
	"net/http"
	"strconv"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (h *handlers) getTree(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(chi.URLParam(r, "repositoryID"), 10, 64)
	if err != nil {
		return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "invalid repository id")
	}

	ref := r.URL.Query().Get("ref")
	treePath := r.URL.Query().Get("path")

	opts := domain.TreeOptions{}
	if values, ok := r.URL.Query()["include"]; ok {
		if len(values) != 1 || values[0] != "lastCommit" {
			return response.NewError(http.StatusBadRequest, response.CodeInvalidRequest, "include must be 'lastCommit'")
		}
		opts.IncludeLastCommit = true
	}

	tree, err := h.service.Tree(r.Context(), id, ref, treePath, opts)
	if err != nil {
		return h.repositoriesError(err, "failed to read repository tree")
	}

	payload, err := newTree(tree)
	if err != nil {
		h.logger.Error("failed to encode repository tree", zap.Error(err))
		return response.NewError(http.StatusInternalServerError, response.CodeInternalError, "failed to read repository tree")
	}
	return response.Data(w, http.StatusOK, payload)
}
