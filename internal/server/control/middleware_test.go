package control

import "testing"

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header string
		token  string
		ok     bool
	}{
		{"Bearer abc123", "abc123", true},
		{"bearer abc123", "abc123", true}, // scheme is case-insensitive
		{"Bearer ", "", false},            // empty token
		{"abc123", "", false},             // no scheme
		{"", "", false},
		{"Basic abc123", "", false}, // wrong scheme
	}

	for _, tc := range cases {
		token, ok := bearerToken(tc.header)
		if token != tc.token || ok != tc.ok {
			t.Errorf("bearerToken(%q) = %q,%v; want %q,%v", tc.header, token, ok, tc.token, tc.ok)
		}
	}
}
