package domain

import "testing"

func TestNormalizePathPattern(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"runtime", "runtime", true},
		{"runtime/", "runtime", true},
		{"/runtime/", "runtime", true},
		{"a/b/c", "a/b/c", true},
		{"config.lock", "config.lock", true},
		{"", "", false},
		{"/", "", false},
		{".", "", false},
		{"..", "", false},
		{"a/../b", "", false},
		{"a//b", "", false},
		{"a/./b", "", false},
		{"a\nb", "", false},
		{"a\x00b", "", false},
	}

	for _, tc := range cases {
		got, ok := NormalizePathPattern(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizePathPattern(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPathBlocked(t *testing.T) {
	patterns := []string{"runtime", "docs/generated", "config.lock"}

	cases := []struct {
		path    string
		pattern string
		blocked bool
	}{
		{"runtime", "runtime", true},
		{"runtime/state.json", "runtime", true},
		{"runtime/deep/nested/file", "runtime", true},
		{"docs/generated/api.md", "docs/generated", true},
		{"docs/generated", "docs/generated", true},
		{"config.lock", "config.lock", true},

		{"runtime.md", "", false},   // prefix of the name, not the path
		{"runtimes/x", "", false},   // sibling directory
		{"docs/gen", "", false},     // partial segment
		{"src/runtime/x", "", false}, // patterns anchor at the repo root
		{"config.lock2", "", false},
		{"README.md", "", false},
	}

	for _, tc := range cases {
		pattern, blocked := PathBlocked(patterns, tc.path)
		if blocked != tc.blocked || pattern != tc.pattern {
			t.Errorf("PathBlocked(%q) = (%q, %v), want (%q, %v)", tc.path, pattern, blocked, tc.pattern, tc.blocked)
		}
	}
}
