package control

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// how long the readiness probe waits for the database before giving up
const healthCheckTimeout = 2 * time.Second

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")

	if err := s.health.Health(ctx); err != nil {
		s.logger.Warn("health check failed", zap.Error(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
