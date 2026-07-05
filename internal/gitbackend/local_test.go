package gitbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

func TestNormalizeRev(t *testing.T) {
	cases := []struct {
		name, rev, want string
		wantErr         error
	}{
		{"empty defaults to HEAD", "", "HEAD", nil},
		{"branch", "main", "main", nil},
		{"expression", "main~2", "main~2", nil},
		{"option injection", "--help", "", ErrInvalidRev},
		{"leading dash", "-x", "", ErrInvalidRev},
		{"newline", "main\nx", "", ErrInvalidRev},
		{"nul", "main\x00", "", ErrInvalidRev},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeRev(tc.rev)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("normalizeRev(%q) error = %v, want %v", tc.rev, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("normalizeRev(%q) = %q, want %q", tc.rev, got, tc.want)
			}
		})
	}
}

func TestNormalizeTreePath(t *testing.T) {
	cases := []struct {
		name, path, want string
		wantErr          error
	}{
		{"empty is root", "", "", nil},
		{"dot is root", ".", "", nil},
		{"slash is root", "/", "", nil},
		{"plain", "src", "src", nil},
		{"trailing slash", "src/", "src", nil},
		{"traversal contained", "../../etc", "etc", nil},
		{"dotdot in middle", "a/../b", "b", nil},
		{"nul", "src\x00", "", ErrInvalidPath},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeTreePath(tc.path)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("normalizeTreePath(%q) error = %v, want %v", tc.path, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("normalizeTreePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestParseLsTree(t *testing.T) {
	// real `ls-tree --long -z` framing: size right-padded to 7, "-" for non-blobs
	out := []byte("100644 blob aaaa      30\tmain.go\x00" +
		"040000 tree bbbb       -\tsub dir\x00" +
		"100755 blob cccc     123\twith\ttab\x00")

	entries, truncated, err := parseLsTree(out, "src")
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Error("unexpected truncation")
	}

	want := []TreeEntry{
		{Mode: "100644", Type: "blob", SHA: "aaaa", Size: 30, Path: "src/main.go"},
		{Mode: "040000", Type: "tree", SHA: "bbbb", Size: -1, Path: "src/sub dir"},
		{Mode: "100755", Type: "blob", SHA: "cccc", Size: 123, Path: "src/with\ttab"},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestParseLsTreeTruncates(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < maxTreeEntries+5; i++ {
		fmt.Fprintf(&sb, "100644 blob aaaa       1\tf%d\x00", i)
	}

	entries, truncated, err := parseLsTree([]byte(sb.String()), "")
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("want truncated listing")
	}
	if len(entries) != maxTreeEntries {
		t.Errorf("got %d entries, want cap %d", len(entries), maxTreeEntries)
	}
}

// end-to-end against a real git binary, following the repo convention of
// skipping when git is not on PATH
func TestListTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := l.InitBare(ctx, "1/test.git"); err != nil {
		t.Fatal(err)
	}

	// empty repo: HEAD is unborn
	if _, err := l.ListTree(ctx, "1/test.git", "", ""); !errors.Is(err, ErrRevNotFound) {
		t.Fatalf("empty repo: want ErrRevNotFound, got %v", err)
	}

	// build a working tree and push it to a known branch name
	wt := filepath.Join(t.TempDir(), "wt")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git(".", "clone", filepath.Join(root, "1/test.git"), wt)
	writeFile := func(rel, content string, mode os.FileMode) {
		t.Helper()
		full := filepath.Join(wt, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("README.md", "hello\n", 0o644)
	writeFile("src/main.go", "package main\n", 0o644)
	writeFile("src/run.sh", "#!/bin/sh\n", 0o755)
	git(wt, "add", "-A")
	git(wt, "commit", "-m", "init")
	git(wt, "push", "origin", "HEAD:refs/heads/main")

	t.Run("root listing", func(t *testing.T) {
		listing, err := l.ListTree(ctx, "1/test.git", "main", "")
		if err != nil {
			t.Fatal(err)
		}
		if listing.CommitSHA == "" {
			t.Error("want resolved commit sha")
		}
		if listing.Truncated {
			t.Error("unexpected truncation")
		}
		if len(listing.Entries) != 2 {
			t.Fatalf("want 2 entries, got %+v", listing.Entries)
		}
		if listing.Entries[0].Path != "README.md" || listing.Entries[0].Type != "blob" || listing.Entries[0].Size != 6 {
			t.Errorf("README.md entry = %+v", listing.Entries[0])
		}
		if listing.Entries[1].Path != "src" || listing.Entries[1].Type != "tree" || listing.Entries[1].Size != -1 {
			t.Errorf("src entry = %+v", listing.Entries[1])
		}
	})

	t.Run("subdir listing", func(t *testing.T) {
		listing, err := l.ListTree(ctx, "1/test.git", "main", "src")
		if err != nil {
			t.Fatal(err)
		}
		if len(listing.Entries) != 2 {
			t.Fatalf("want 2 entries, got %+v", listing.Entries)
		}
		if listing.Entries[0].Path != "src/main.go" {
			t.Errorf("want path src/main.go, got %q", listing.Entries[0].Path)
		}
		if listing.Entries[1].Mode != "100755" {
			t.Errorf("run.sh: want mode 100755, got %q", listing.Entries[1].Mode)
		}
	})

	t.Run("errors", func(t *testing.T) {
		cases := []struct {
			name, rev, path string
			want            error
		}{
			{"unknown rev", "nope", "", ErrRevNotFound},
			{"unknown path", "main", "nope", ErrPathNotFound},
			{"path is a blob", "main", "README.md", ErrPathNotFound},
			{"hostile rev", "--help", "", ErrInvalidRev},
			{"traversal path", "main", "../../etc", ErrPathNotFound},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := l.ListTree(ctx, "1/test.git", tc.rev, tc.path); !errors.Is(err, tc.want) {
					t.Errorf("ListTree(%q, %q) = %v, want %v", tc.rev, tc.path, err, tc.want)
				}
			})
		}
	})
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
