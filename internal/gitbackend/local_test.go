package gitbackend

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"time"
)

// runs a git command in dir with a deterministic identity, failing the test on error
func gitRun(t *testing.T, dir string, args ...string) {
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

// like gitRun but returns the trimmed stdout
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

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
	gitRun(t, ".", "clone", filepath.Join(root, "1/test.git"), wt)
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
	gitRun(t, wt, "add", "-A")
	gitRun(t, wt, "commit", "-m", "init")
	gitRun(t, wt, "push", "origin", "HEAD:refs/heads/main")

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

func TestArchiveTar(t *testing.T) {
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
	if _, err := l.ArchiveTar(ctx, "1/test.git", "", io.Discard); !errors.Is(err, ErrRevNotFound) {
		t.Fatalf("empty repo: want ErrRevNotFound, got %v", err)
	}

	// an LFS-pointer-shaped blob: the archive must carry it byte-for-byte,
	// smudging is explicitly not this layer's job
	pointer := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + strings.Repeat("a", 64) + "\n" +
		"size 12345\n"

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, ".", "clone", filepath.Join(root, "1/test.git"), wt)
	files := map[string]string{
		"README.md":   "hello\n",
		"src/main.go": "package main\n",
		"big.bin":     pointer,
	}
	for rel, content := range files {
		full := filepath.Join(wt, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, wt, "add", "-A")
	gitRun(t, wt, "commit", "-m", "init")
	gitRun(t, wt, "push", "origin", "HEAD:refs/heads/main")

	var buf bytes.Buffer
	sha, err := l.ArchiveTar(ctx, "1/test.git", "main", &buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Errorf("want 40-hex commit sha, got %q", sha)
	}

	// the stream must be a valid tar containing exactly the committed files
	got := map[string]string{}
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg { // skip dirs and the pax global header
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = string(body)
	}
	if len(got) != len(files) {
		t.Fatalf("got %d regular files, want %d: %v", len(got), len(files), got)
	}
	for rel, content := range files {
		if got[rel] != content {
			t.Errorf("%s = %q, want %q", rel, got[rel], content)
		}
	}

	if _, err := l.ArchiveTar(ctx, "1/test.git", "nope", io.Discard); !errors.Is(err, ErrRevNotFound) {
		t.Errorf("unknown rev: want ErrRevNotFound, got %v", err)
	}
	if _, err := l.ArchiveTar(ctx, "1/test.git", "--help", io.Discard); !errors.Is(err, ErrInvalidRev) {
		t.Errorf("hostile rev: want ErrInvalidRev, got %v", err)
	}
}

func TestBlob(t *testing.T) {
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

	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, ".", "clone", filepath.Join(root, "1/test.git"), wt)
	if err := os.MkdirAll(filepath.Join(wt, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", "-A")
	gitRun(t, wt, "commit", "-m", "init")
	gitRun(t, wt, "push", "origin", "HEAD:refs/heads/main")

	info, err := l.StatBlob(ctx, "1/test.git", "main", "src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len("package main\n")) {
		t.Errorf("size = %d", info.Size)
	}
	if !isHexSHA(info.CommitSHA) || !isHexSHA(info.BlobSHA) {
		t.Errorf("shas not resolved: %+v", info)
	}

	var buf bytes.Buffer
	if err := l.ReadBlob(ctx, "1/test.git", info.BlobSHA, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "package main\n" {
		t.Errorf("content = %q", buf.String())
	}

	t.Run("write blob", func(t *testing.T) {
		// git blob shas are deterministic: "hello\n" is famously 0xce0136...
		sha, size, err := l.WriteBlob(ctx, "1/test.git", strings.NewReader("hello\n"))
		if err != nil {
			t.Fatal(err)
		}
		if sha != "ce013625030ba8dba906f756967f9e9ca394464a" {
			t.Errorf("sha = %q", sha)
		}
		if size != 6 {
			t.Errorf("size = %d", size)
		}

		// the object must be readable back before any commit references it
		var buf bytes.Buffer
		if err := l.ReadBlob(ctx, "1/test.git", sha, &buf); err != nil {
			t.Fatal(err)
		}
		if buf.String() != "hello\n" {
			t.Errorf("content = %q", buf.String())
		}

		// uploading the same content again dedupes to the same sha
		again, _, err := l.WriteBlob(ctx, "1/test.git", strings.NewReader("hello\n"))
		if err != nil {
			t.Fatal(err)
		}
		if again != sha {
			t.Errorf("re-upload sha = %q, want %q", again, sha)
		}

		// empty content is the canonical empty blob
		empty, size, err := l.WriteBlob(ctx, "1/test.git", strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		if empty != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" || size != 0 {
			t.Errorf("empty blob = %q size %d", empty, size)
		}
	})

	t.Run("errors", func(t *testing.T) {
		cases := []struct {
			name, rev, path string
			want            error
		}{
			{"root is a tree", "main", "", ErrNotABlob},
			{"dir is a tree", "main", "src", ErrNotABlob},
			{"missing path", "main", "nope.txt", ErrPathNotFound},
			{"unknown rev", "nope", "src/main.go", ErrRevNotFound},
			{"hostile rev", "--help", "src/main.go", ErrInvalidRev},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := l.StatBlob(ctx, "1/test.git", tc.rev, tc.path); !errors.Is(err, tc.want) {
					t.Errorf("StatBlob(%q, %q) = %v, want %v", tc.rev, tc.path, err, tc.want)
				}
			})
		}

		// ReadBlob refuses anything that is not a plain object id
		for _, sha := range []string{"main", "--help", "HEAD", strings.Repeat("a", 39), strings.Repeat("A", 40)} {
			if err := l.ReadBlob(ctx, "1/test.git", sha, io.Discard); !errors.Is(err, ErrInvalidRev) {
				t.Errorf("ReadBlob(%q) = %v, want ErrInvalidRev", sha, err)
			}
		}
	})
}

func TestApplyCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const repo = "1/test.git"

	if err := l.InitBare(ctx, repo); err != nil {
		t.Fatal(err)
	}

	author := Identity{Name: "api-user", Email: "api@test"}
	spec := func(expectedOld, msg string) CommitSpec {
		return CommitSpec{Branch: "main", ExpectedOld: expectedOld, Author: author, Message: msg}
	}
	blob := func(content string) string {
		t.Helper()
		sha, _, err := l.WriteBlob(ctx, repo, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}

	hello := blob("hello\n")
	script := blob("#!/bin/sh\n")

	// creating a branch requires explicitly expecting non-existence
	if _, err := l.ApplyCommit(ctx, repo, spec("", "init"), []CommitOp{{Path: "README.md", BlobSHA: hello}}, nil); !errors.Is(err, ErrRevNotFound) {
		t.Fatalf("missing branch without zero expected-old: want ErrRevNotFound, got %v", err)
	}

	first, err := l.ApplyCommit(ctx, repo, spec(zeroSHA, "init"), []CommitOp{
		{Path: "README.md", BlobSHA: hello},
		{Path: "src/run.sh", BlobSHA: script, Mode: "100755"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.OldSHA != zeroSHA || !isHexSHA(first.NewSHA) || first.Ref != "refs/heads/main" {
		t.Fatalf("first change = %+v", first)
	}

	// a real git client must see exactly what we committed
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, ".", "clone", "-b", "main", filepath.Join(root, repo), wt)

	readme, err := os.ReadFile(filepath.Join(wt, "README.md"))
	if err != nil || string(readme) != "hello\n" {
		t.Errorf("README.md = %q, %v", readme, err)
	}
	info, err := os.Stat(filepath.Join(wt, "src", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("run.sh not executable: %v", info.Mode())
	}

	logOut := gitOut(t, wt, "log", "-1", "--format=%H|%an|%ae|%s")
	if logOut != first.NewSHA+"|api-user|api@test|init" {
		t.Errorf("log = %q", logOut)
	}

	// second commit: CAS on the known head, update one file, delete another
	v2 := blob("hello v2\n")
	second, err := l.ApplyCommit(ctx, repo, spec(first.NewSHA, "update"), []CommitOp{
		{Path: "README.md", BlobSHA: v2},
		{Path: "src/run.sh", Delete: true},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.OldSHA != first.NewSHA {
		t.Errorf("second change = %+v", second)
	}

	gitRun(t, wt, "pull")
	if readme, _ := os.ReadFile(filepath.Join(wt, "README.md")); string(readme) != "hello v2\n" {
		t.Errorf("README.md after pull = %q", readme)
	}
	if _, err := os.Stat(filepath.Join(wt, "src", "run.sh")); !os.IsNotExist(err) {
		t.Errorf("run.sh should be deleted, stat err = %v", err)
	}

	t.Run("errors", func(t *testing.T) {
		cases := []struct {
			name string
			spec CommitSpec
			ops  []CommitOp
			want error
		}{
			{"stale cas", spec(first.NewSHA, "x"), []CommitOp{{Path: "a", BlobSHA: hello}}, ErrHeadMismatch},
			{"create existing branch", spec(zeroSHA, "x"), []CommitOp{{Path: "a", BlobSHA: hello}}, ErrHeadMismatch},
			{"unknown blob", spec("", "x"), []CommitOp{{Path: "a", BlobSHA: strings.Repeat("d", 40)}}, ErrUnknownBlob},
			{"nothing to commit", spec("", "x"), []CommitOp{{Path: "README.md", BlobSHA: v2}}, ErrNothingToCommit},
			{"delete missing path", spec("", "x"), []CommitOp{{Path: "nope.txt", Delete: true}}, ErrPathNotFound},
			{"bad branch", CommitSpec{Branch: "a..b", ExpectedOld: "", Author: author, Message: "x"}, []CommitOp{{Path: "a", BlobSHA: hello}}, ErrInvalidBranch},
			{"hostile branch", CommitSpec{Branch: "--help", Author: author, Message: "x"}, []CommitOp{{Path: "a", BlobSHA: hello}}, ErrInvalidBranch},
			{"no ops", spec("", "x"), nil, ErrInvalidOps},
			{"duplicate path", spec("", "x"), []CommitOp{{Path: "a", BlobSHA: hello}, {Path: "a", Delete: true}}, ErrInvalidOps},
			{"bad mode", spec("", "x"), []CommitOp{{Path: "a", BlobSHA: hello, Mode: "120000"}}, ErrInvalidOps},
			{"missing author", CommitSpec{Branch: "main", Author: Identity{}, Message: "x"}, []CommitOp{{Path: "a", BlobSHA: hello}}, ErrInvalidOps},
			{"missing message", CommitSpec{Branch: "main", Author: author}, []CommitOp{{Path: "a", BlobSHA: hello}}, ErrInvalidOps},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := l.ApplyCommit(ctx, repo, tc.spec, tc.ops, nil); !errors.Is(err, tc.want) {
					t.Errorf("ApplyCommit = %v, want %v", err, tc.want)
				}
			})
		}
	})

	t.Run("lfs clean", func(t *testing.T) {
		attrs := blob("*.bin filter=lfs diff=lfs merge=lfs -text\n")
		payload := blob("REAL BINARY CONTENT")
		pointerText := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("ab", 32) + "\nsize 19\n"
		pointer := blob(pointerText)

		// tracked path without a clean filter fails loudly
		if _, err := l.ApplyCommit(ctx, repo, spec("", "track"), []CommitOp{
			{Path: ".gitattributes", BlobSHA: attrs},
			{Path: "big.bin", BlobSHA: payload},
		}, nil); !errors.Is(err, ErrLFSRequired) {
			t.Fatalf("want ErrLFSRequired, got %v", err)
		}

		// tracking added in the SAME commit must clean the file next to it
		var gotPath, gotSHA string
		var gotSize int64
		clean := func(path, blobSHA string, size int64) (string, error) {
			gotPath, gotSHA, gotSize = path, blobSHA, size
			return pointer, nil
		}
		change, err := l.ApplyCommit(ctx, repo, spec("", "track + add"), []CommitOp{
			{Path: ".gitattributes", BlobSHA: attrs},
			{Path: "big.bin", BlobSHA: payload},
			{Path: "notes.txt", BlobSHA: hello}, // untracked, must NOT be cleaned
		}, clean)
		if err != nil {
			t.Fatal(err)
		}
		if gotPath != "big.bin" || gotSHA != payload || gotSize != 19 {
			t.Errorf("clean called with (%q, %q, %d)", gotPath, gotSHA, gotSize)
		}

		// the committed tree holds the pointer, not the payload
		committed, err := l.StatBlob(ctx, repo, change.NewSHA, "big.bin")
		if err != nil {
			t.Fatal(err)
		}
		if committed.BlobSHA != pointer {
			t.Errorf("big.bin blob = %s, want pointer %s", committed.BlobSHA, pointer)
		}
		notes, err := l.StatBlob(ctx, repo, change.NewSHA, "notes.txt")
		if err != nil {
			t.Fatal(err)
		}
		if notes.BlobSHA != hello {
			t.Errorf("notes.txt was cleaned but is not lfs-tracked")
		}
	})
}

func TestGC(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const repo = "1/test.git"

	if err := l.InitBare(ctx, repo); err != nil {
		t.Fatal(err)
	}

	// a fresh orphan must survive gc: the prune grace period protects
	// uploads whose commit has not happened yet
	fresh, _, err := l.WriteBlob(ctx, repo, strings.NewReader("pending upload\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.GC(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := l.ReadBlob(ctx, repo, fresh, io.Discard); err != nil {
		t.Fatalf("fresh orphan pruned: %v", err)
	}

	// an orphan backdated past gc.pruneExpire (2 weeks) must be pruned;
	// backdate before gc runs, since gc moves loose objects into cruft packs
	orphan, _, err := l.WriteBlob(ctx, repo, strings.NewReader("abandoned upload\n"))
	if err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(root, repo, "objects", orphan[:2], orphan[2:])
	old := time.Now().Add(-21 * 24 * time.Hour)
	if err := os.Chtimes(loose, old, old); err != nil {
		t.Fatal(err)
	}

	if err := l.GC(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := l.ReadBlob(ctx, repo, orphan, io.Discard); err == nil {
		t.Error("expired orphan survived gc")
	}
}

func TestPreReceive(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const repo = "1/test.git"
	repoDir := filepath.Join(root, repo)

	if err := l.InitBare(ctx, repo); err != nil {
		t.Fatal(err)
	}

	author := Identity{Name: "t", Email: "t@t"}
	policies := []domain.PathPolicy{{Pattern: "runtime", Reason: "deploy-managed state"}}

	blob := func(content string) string {
		t.Helper()
		sha, _, err := l.WriteBlob(ctx, repo, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}

	// stage builds commits on a scratch branch and then deletes the ref:
	// the commits stay in the odb but become unreachable, exactly the state
	// a pre-receive hook sees while a push is quarantined
	stage := func(base string, ops []CommitOp) string {
		t.Helper()
		expected := zeroSHA
		if base != "" {
			expected = ""
			gitRun(t, repoDir, "update-ref", "refs/heads/scratch", base)
		}
		change, err := l.ApplyCommit(ctx, repo, CommitSpec{Branch: "scratch", ExpectedOld: expected, Author: author, Message: "staged"}, ops, nil)
		if err != nil {
			t.Fatal(err)
		}
		gitRun(t, repoDir, "update-ref", "-d", "refs/heads/scratch")
		return change.NewSHA
	}

	line := func(newSHA string) io.Reader {
		return strings.NewReader(zeroSHA + " " + newSHA + " refs/heads/main\n")
	}

	t.Run("blocked path is rejected with the reason", func(t *testing.T) {
		sha := stage("", []CommitOp{
			{Path: "README.md", BlobSHA: blob("ok\n")},
			{Path: "runtime/state.json", BlobSHA: blob("{}\n")},
		})
		err := PreReceive(ctx, repoDir, line(sha), policies)
		if err == nil || !strings.Contains(err.Error(), "deploy-managed state") {
			t.Errorf("want rejection with reason, got %v", err)
		}
	})

	t.Run("clean push is allowed", func(t *testing.T) {
		sha := stage("", []CommitOp{{Path: "src/main.go", BlobSHA: blob("package main\n")}})
		if err := PreReceive(ctx, repoDir, line(sha), policies); err != nil {
			t.Errorf("clean push rejected: %v", err)
		}
	})

	t.Run("violation in an intermediate commit is caught", func(t *testing.T) {
		// commit 1 adds the blocked path, commit 2 removes it again: the net
		// diff is clean but the content would live in history forever
		first := stage("", []CommitOp{{Path: "runtime/state.json", BlobSHA: blob("leak\n")}})
		second := stage(first, []CommitOp{{Path: "runtime/state.json", Delete: true}, {Path: "ok.txt", BlobSHA: blob("x\n")}})
		if err := PreReceive(ctx, repoDir, line(second), policies); err == nil {
			t.Error("intermediate violation slipped through")
		}
	})

	t.Run("deleting a blocked file is allowed", func(t *testing.T) {
		// the blocked path already exists on a real branch (added before the
		// policy); a push that only deletes it must pass
		base, err := l.ApplyCommit(ctx, repo, CommitSpec{Branch: "cleanup", ExpectedOld: zeroSHA, Author: author, Message: "pre-policy"},
			[]CommitOp{{Path: "runtime/state.json", BlobSHA: blob("old\n")}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		sha := stage(base.NewSHA, []CommitOp{{Path: "runtime/state.json", Delete: true}})
		if err := PreReceive(ctx, repoDir, line(sha), policies); err != nil {
			t.Errorf("cleanup push rejected: %v", err)
		}
	})

	t.Run("ref deletion is allowed", func(t *testing.T) {
		in := strings.NewReader(strings.Repeat("a", 40) + " " + zeroSHA + " refs/heads/gone\n")
		if err := PreReceive(ctx, repoDir, in, policies); err != nil {
			t.Errorf("ref deletion rejected: %v", err)
		}
	})

	t.Run("no policies short-circuits", func(t *testing.T) {
		if err := PreReceive(ctx, repoDir, strings.NewReader("garbage that is never read"), nil); err != nil {
			t.Errorf("no-policy push rejected: %v", err)
		}
	})

	t.Run("malformed input fails closed", func(t *testing.T) {
		if err := PreReceive(ctx, repoDir, strings.NewReader("what\n"), policies); err == nil {
			t.Error("malformed input must reject")
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
