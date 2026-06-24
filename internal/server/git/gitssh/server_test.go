package gitssh

import "testing"

func TestSplitRepoPath(t *testing.T) {
	cases := []struct {
		in      string
		ns      string
		name    string
		wantErr bool
	}{
		{"acme/api.git", "acme", "api", false},
		{"/acme/api.git", "acme", "api", false},
		{"acme/api", "acme", "api", false},
		{"api.git", "", "", true},   // single segment
		{"a/b/c.git", "", "", true}, // too many segments
		{"", "", "", true},
	}

	for _, tc := range cases {
		ns, name, err := splitRepoPath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("splitRepoPath(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitRepoPath(%q): unexpected error %v", tc.in, err)
			continue
		}
		if ns != tc.ns || name != tc.name {
			t.Errorf("splitRepoPath(%q) = %q,%q; want %q,%q", tc.in, ns, name, tc.ns, tc.name)
		}
	}
}

func TestParseGitCommand(t *testing.T) {
	cases := []struct {
		in      string
		sub     string
		repo    string
		wantErr bool
	}{
		{"git-upload-pack 'acme/api.git'", "git-upload-pack", "acme/api.git", false},
		{"git-receive-pack 'x.git'", "git-receive-pack", "x.git", false},
		{"git-upload-pack", "", "", true}, // missing repo arg (single field)
		{"bash", "", "", true},            // single field, no repo
	}

	for _, tc := range cases {
		sub, repo, err := parseGitCommand(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseGitCommand(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseGitCommand(%q): unexpected error %v", tc.in, err)
			continue
		}
		if sub != tc.sub || repo != tc.repo {
			t.Errorf("parseGitCommand(%q) = %q,%q; want %q,%q", tc.in, sub, repo, tc.sub, tc.repo)
		}
	}
}
