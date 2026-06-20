package githttp

import (
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Handlers struct {
	logger *zap.Logger
	// services here then
}

func NewHandlers(logger *zap.Logger) *Handlers {
	return &Handlers{
		logger: logger,
	}
}

func (h *Handlers) RegisterRoutes(githttp chi.Router) {
	// githttp.Handle("/whatever")
}
