package control

import (
	"net/http"
	"strings"

	"github.com/Axenos-dev/HeadlessGit/internal/server/audit"
	"github.com/Axenos-dev/HeadlessGit/internal/server/response"
)

// requires a valid bearer token that resolves to an admin account
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			response.WriteError(w, s.logger, response.NewError(http.StatusUnauthorized, response.CodeUnauthorized, "missing bearer token"))
			return
		}

		account, err := s.auth.AuthenticateToken(r.Context(), token)
		if err != nil {
			response.WriteError(w, s.logger, response.NewError(http.StatusUnauthorized, response.CodeUnauthorized, "invalid token"))
			return
		}

		if !account.IsAdmin {
			response.WriteError(w, s.logger, response.NewError(http.StatusForbidden, response.CodeForbidden, "admin access required"))
			return
		}

		// pass identity to audit even if request is authenticated
		if e := audit.FromContext(r.Context()); e != nil {
			e.IdentityID = account.UserID
		}

		next.ServeHTTP(w, r)
	})
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
