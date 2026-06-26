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

func TestParseLFSAuthCommand(t *testing.T) {
	cases := []struct {
		in      string
		repo    string
		op      string
		wantErr bool
	}{
		{"git-lfs-authenticate 'acme/api.git' download", "acme/api.git", "download", false},
		{"git-lfs-authenticate acme/api.git upload", "acme/api.git", "upload", false},
		{"git-lfs-authenticate 'acme/api.git'", "", "", true},              // missing operation
		{"git-lfs-authenticate acme/api.git download extra", "", "", true}, // too many args
		{"git-upload-pack 'acme/api.git'", "", "", true},                   // wrong subcommand
		{"", "", "", true},
	}

	for _, tc := range cases {
		repo, op, err := parseLFSAuthCommand(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseLFSAuthCommand(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLFSAuthCommand(%q): unexpected error %v", tc.in, err)
			continue
		}
		if repo != tc.repo || op != tc.op {
			t.Errorf("parseLFSAuthCommand(%q) = %q,%q; want %q,%q", tc.in, repo, op, tc.repo, tc.op)
		}
	}
}
