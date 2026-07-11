package domain

import (
	"strings"
	"testing"
)

func TestNormalizeArchivePrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   string
		ok     bool
	}{
		{"empty", "", "", true},
		{"directory", "release", "release/", true},
		{"nested", "release/source", "release/source/", true},
		{"trailing slash", "release/source/", "release/source/", true},
		{"unicode", "releases/été", "releases/été/", true},
		{"absolute", "/release", "", false},
		{"dot segment", "release/./source", "", false},
		{"parent segment", "release/../source", "", false},
		{"empty segment", "release//source", "", false},
		{"multiple trailing slashes", "release//", "", false},
		{"backslash", `release\source`, "", false},
		{"windows volume", "C:/release", "", false},
		{"control character", "release\nsource", "", false},
		{"too long", strings.Repeat("a", 256), "", false},
		{"invalid utf8", string([]byte{0xff}), "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeArchivePrefix(tc.prefix)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("NormalizeArchivePrefix(%q) = %q, %v; want %q, %v", tc.prefix, got, ok, tc.want, tc.ok)
			}
		})
	}
}
