package response

import (
	"errors"
	"net/http"

	"go.uber.org/zap"
)

type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// basic wrapper for handlers
// allows us easily handle error from the handler, and wrap it in the envelope and log
func Handler(logger *zap.Logger, h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}

		if apiErr, ok := errors.AsType[*APIError](err); ok {
			writeJSON(w, apiErr.Status, errorEnvelope{Error: apiErr})
			return
		}

		logger.Error("unhandled error", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, errorEnvelope{Error: &APIError{
			Code:    CodeInternalError,
			Message: "internal server error",
		}})
	}
}
