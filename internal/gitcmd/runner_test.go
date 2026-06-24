package gitcmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveContainment(t *testing.T) {
	root := "/srv/repos"
	r := &Runner{root: root}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"normal", "acme/api.git", "/srv/repos/acme/api.git"},
		{"leading slash", "/acme/api.git", "/srv/repos/acme/api.git"},
		{"parent traversal", "../../etc/passwd", "/srv/repos/etc/passwd"},
		{"dotdot in middle", "a/../../b.git", "/srv/repos/b.git"},
		{"empty", "", "/srv/repos"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.resolve(tc.path)
			if err != nil {
				t.Fatalf("resolve(%q) error: %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("resolve(%q) = %q, want %q", tc.path, got, tc.want)
			}
			// the invariant that matters: the result never escapes the root
			if got != root && !strings.HasPrefix(got, root+string(filepath.Separator)) {
				t.Errorf("resolve(%q) = %q escaped root %q", tc.path, got, root)
			}
		})
	}
}
