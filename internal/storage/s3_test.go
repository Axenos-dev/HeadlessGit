package storage

import (
	"strings"
	"testing"
)

func TestS3Key(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{"no prefix", "", "12/ab/cd/oid", "12/ab/cd/oid"},
		{"with prefix", "lfs", "12/ab/cd/oid", "lfs/12/ab/cd/oid"},
		{"prefix trimmed", "/lfs/", "12/ab/cd/oid", "lfs/12/ab/cd/oid"},
		{"nested prefix", "team/lfs", "12/ab/cd/oid", "team/lfs/12/ab/cd/oid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &S3{prefix: strings.Trim(tc.prefix, "/")}
			if got := s.key(tc.key); got != tc.want {
				t.Errorf("key(%q) with prefix %q = %q, want %q", tc.key, tc.prefix, got, tc.want)
			}
		})
	}
}
