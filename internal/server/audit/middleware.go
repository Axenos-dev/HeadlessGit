package audit

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func Middleware(logger *zap.Logger, transport string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			e := &Event{
				RequestID: middleware.GetReqID(r.Context()),
				Transport: transport,
			}

			// capture the response status so result can be derived from it
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// and derive the result on defer
			// when request is done
			defer func() {
				if e.Result == "" {
					e.Result = resultFromStatus(ww.Status())
				}
				Log(logger, e, time.Since(start))
			}()

			next.ServeHTTP(ww, r.WithContext(NewContext(r.Context(), e)))
		})
	}
}

func resultFromStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return "denied"
	case status >= 400:
		return "error"
	default:
		return "ok"
	}
}
