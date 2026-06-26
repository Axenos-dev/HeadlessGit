package smart

import (
	"net/http/httptest"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

func TestRequiredRole(t *testing.T) {
	cases := []struct {
		name    string
		service string
		query   string
		want    domain.Role
	}{
		{"upload-pack", "/git-upload-pack", "", domain.RoleRead},
		{"receive-pack", "/git-receive-pack", "", domain.RoleWrite},
		{"info/refs upload", "/info/refs", "service=git-upload-pack", domain.RoleRead},
		{"info/refs receive", "/info/refs", "service=git-receive-pack", domain.RoleWrite},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/x?"+tc.query, nil)
			if got := requiredRole(req, tc.service); got != tc.want {
				t.Errorf("requiredRole(%q, ?%s) = %q, want %q", tc.service, tc.query, got, tc.want)
			}
		})
	}
}
