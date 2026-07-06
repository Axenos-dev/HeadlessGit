package domain

import (
	"strings"
	"testing"
)

func TestParseLFSPointer(t *testing.T) {
	oid := strings.Repeat("ab", 32)
	valid := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 12345\n"

	cases := []struct {
		name string
		data string
		ok   bool
	}{
		{"valid", valid, true},
		{"empty", "", false},
		{"plain text", "hello world\nthis mentions oid sha256: things\nsize 5\n", false},
		{"wrong version", "version https://example.com/spec/v1\noid sha256:" + oid + "\nsize 5\n", false},
		{"short oid", "version https://git-lfs.github.com/spec/v1\noid sha256:abcd\nsize 5\n", false},
		{"uppercase oid", "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.ToUpper(oid) + "\nsize 5\n", false},
		{"missing size", "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\n\n", false},
		{"bad size", "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize lots\n", false},
		{"oversized", valid + strings.Repeat("x", LFSPointerMaxSize), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ptr, ok := ParseLFSPointer([]byte(tc.data))
			if ok != tc.ok {
				t.Fatalf("ParseLFSPointer ok = %v, want %v", ok, tc.ok)
			}
			if ok && (ptr.OID != oid || ptr.Size != 12345) {
				t.Errorf("ParseLFSPointer = %+v", ptr)
			}
		})
	}
}
