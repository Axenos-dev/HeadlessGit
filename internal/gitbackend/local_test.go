package gitbackend

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestResolveContainment(t *testing.T) {
	root := "/srv/repos"
	l := &Local{root: root}

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
			got, err := l.resolve(tc.path)
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

func TestDiffRefs(t *testing.T) {
	cases := []struct {
		name          string
		before, after map[string]string
		want          []RefChange
	}{
		{
			name:   "no change",
			before: map[string]string{"refs/heads/main": "aaa"},
			after:  map[string]string{"refs/heads/main": "aaa"},
			want:   nil,
		},
		{
			name:   "update",
			before: map[string]string{"refs/heads/main": "aaa"},
			after:  map[string]string{"refs/heads/main": "bbb"},
			want:   []RefChange{{Ref: "refs/heads/main", OldSHA: "aaa", NewSHA: "bbb"}},
		},
		{
			name:   "create",
			before: map[string]string{},
			after:  map[string]string{"refs/heads/dev": "ccc"},
			want:   []RefChange{{Ref: "refs/heads/dev", OldSHA: zeroSHA, NewSHA: "ccc"}},
		},
		{
			name:   "delete",
			before: map[string]string{"refs/tags/v1": "ddd"},
			after:  map[string]string{},
			want:   []RefChange{{Ref: "refs/tags/v1", OldSHA: "ddd", NewSHA: zeroSHA}},
		},
		{
			name:   "mixed",
			before: map[string]string{"refs/heads/main": "aaa", "refs/tags/v1": "ddd"},
			after:  map[string]string{"refs/heads/main": "bbb", "refs/heads/dev": "ccc"},
			want: []RefChange{
				{Ref: "refs/heads/dev", OldSHA: zeroSHA, NewSHA: "ccc"},
				{Ref: "refs/heads/main", OldSHA: "aaa", NewSHA: "bbb"},
				{Ref: "refs/tags/v1", OldSHA: "ddd", NewSHA: zeroSHA},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DiffRefs(tc.before, tc.after)

			// map iteration order is unstable, so sort before comparing
			sort.Slice(got, func(i, j int) bool { return got[i].Ref < got[j].Ref })

			if len(got) != len(tc.want) {
				t.Fatalf("got %d changes, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("change %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
