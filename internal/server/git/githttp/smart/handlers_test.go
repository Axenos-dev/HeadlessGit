package smart

import (
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
)

func TestParseServiceAndRole(t *testing.T) {
	cases := []struct {
		name    string
		service string
		wantOK  bool
		wantSvc gitbackend.Service
		want    domain.Role
	}{
		{"upload-pack", "git-upload-pack", true, gitbackend.UploadPack, domain.RoleRead},
		{"receive-pack", "git-receive-pack", true, gitbackend.ReceivePack, domain.RoleWrite},
		{"empty", "", false, 0, ""},
		{"unknown", "git-something", false, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, ok := parseService(tc.service)
			if ok != tc.wantOK {
				t.Fatalf("parseService(%q) ok = %v, want %v", tc.service, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if svc != tc.wantSvc {
				t.Errorf("parseService(%q) = %v, want %v", tc.service, svc, tc.wantSvc)
			}
			if got := requiredRole(svc); got != tc.want {
				t.Errorf("requiredRole(%v) = %q, want %q", svc, got, tc.want)
			}
		})
	}
}
