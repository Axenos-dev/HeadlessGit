package middleware

import (
	"context"
	"net/http"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type Authenticator interface {
	AuthenticateToken(ctx context.Context, rawToken string) (domain.Account, error)
}

// key for context
type contextKey string

const accountKey contextKey = "account"

func WithAccount(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, password, ok := r.BasicAuth(); ok {
				account, err := auth.AuthenticateToken(r.Context(), password)
				if err != nil {
					// a token was provided but it's invalid -> reject
					w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), accountKey, &account))
			}

			// no credentials -> anonymous
			next.ServeHTTP(w, r)
		})
	}
}

func AccountFromContext(ctx context.Context) *domain.Account {
	account, _ := ctx.Value(accountKey).(*domain.Account)
	return account
}
