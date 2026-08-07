package gitbackend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Local implementation of git backend
var _ Backend = (*Local)(nil)

// every push in every repo runs this one file, which execs the server binary in hook mode
const preReceiveShim = `#!/bin/sh
# written by headlessgit on startup; do not edit
exec "$HEADLESSGIT_BIN" hook pre-receive
`

// repacking a large repo can take a while
const gcTimeout = 30 * time.Minute

// local implementation of Git backend
// it runs the git pack protocol against bare repos on the local filesystem
type Local struct {
	root     string
	gitPath  string
	hooksDir string
	timeout  time.Duration

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLocal(root string) (*Local, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git binary not found: %w", err)
	}

	hooksDir := filepath.Join(absRoot, ".hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create hooks dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(preReceiveShim), 0o755); err != nil {
		return nil, fmt.Errorf("write pre-receive shim: %w", err)
	}

	return &Local{
		root:     absRoot,
		gitPath:  gitPath,
		hooksDir: hooksDir,
		timeout:  30 * time.Second,
		locks:    make(map[string]*sync.Mutex),
	}, nil
}

func (l *Local) InitBare(ctx context.Context, storagePath string) error {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return err
	}

	// create folder for repo
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	// initiate bare repo in the folder
	cmd := exec.CommandContext(ctx, l.gitPath, "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init --bare: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// just deletes the folder with bare repo
func (l *Local) Remove(ctx context.Context, storagePath string) error {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// writes the ref advertisement for the smart-HTTP info/refs step
func (l *Local) AdvertiseRefs(ctx context.Context, storagePath string, svc Service, stdout io.Writer) error {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, svc.Name(), "--stateless-rpc", "--advertise-refs", dir)
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("advertise refs: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (l *Local) UploadPack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) error {
	return l.pack(ctx, storagePath, UploadPack, stateless, nil, stdin, stdout, stderr)
}

func (l *Local) ReceivePack(ctx context.Context, storagePath string, stateless bool, hookEnv []string, stdin io.Reader, stdout, stderr io.Writer) ([]RefChange, error) {
	// important to lock concurrent pushes
	// as we compare refs before/after for one operation
	unlock := l.lockRepo(storagePath)
	defer unlock()

	// list refs BEFORE the push
	before, beforeErr := l.listRefs(ctx, storagePath)
	// and IGNORE error, as we dont need to block main receive-pack operation

	env := append(hookEnv,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0="+l.hooksDir,
	)

	if err := l.pack(ctx, storagePath, ReceivePack, stateless, env, stdin, stdout, stderr); err != nil {
		return nil, err
	}

	// and check the before refs error after successful push
	if beforeErr != nil {
		return nil, nil
	}

	// and then list refs AFTER the successful push
	after, err := l.listRefs(ctx, storagePath)
	if err != nil {
		return nil, nil
	}

	return DiffRefs(before, after), nil
}

func (l *Local) pack(ctx context.Context, storagePath string, svc Service, stateless bool, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return err
	}

	args := make([]string, 0, 2)

	// stateless makes process handle on request from stdin and return one response in stdout
	// so its used for git over http
	// except ssh, so it its stateful and keeps process alive for active channel
	if stateless {
		args = append(args, "--stateless-rpc")
	}
	args = append(args, dir)

	cmd := exec.CommandContext(ctx, svc.Name(), args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// directly pass client's bytes to process stdin
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// listRefs returns the repo refs as a refname -> object id map
func (l *Local) listRefs(ctx context.Context, storagePath string) (map[string]string, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return nil, err
	}

	out, err := l.runGit(ctx, dir, nil, nil, "for-each-ref", "--format=%(objectname) %(refname)")
	if err != nil {
		return nil, err
	}

	refs := make(map[string]string)
	for line := range strings.SplitSeq(out, "\n") {
		// cut the string by " ", to separate sha from ref
		sha, ref, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		refs[ref] = sha
	}
	return refs, nil
}

func (l *Local) ListTree(ctx context.Context, storagePath, rev, treePath string, opts ListTreeOptions) (TreeListing, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return TreeListing{}, err
	}

	treePath, err = normalizeTreePath(treePath)
	if err != nil {
		return TreeListing{}, err
	}

	commitSHA, err := l.ResolveCommit(ctx, storagePath, rev)
	if err != nil {
		return TreeListing{}, err
	}

	treeish := commitSHA
	if treePath != "" {
		treeish += ":" + treePath
	}

	out, err := l.runGitBytes(ctx, dir, nil, nil, "ls-tree", "--long", "-z", "--end-of-options", treeish)
	if err != nil {
		// the rev already resolved, so this is a missing path or a non-directory
		return TreeListing{}, fmt.Errorf("%w: %q", ErrPathNotFound, treePath)
	}

	entries, truncated, err := parseLsTree(out, treePath)
	if err != nil {
		return TreeListing{}, err
	}
	if opts.IncludeLastCommit && len(entries) > 0 {
		if err := l.addLastCommits(ctx, dir, commitSHA, treePath, entries); err != nil {
			return TreeListing{}, err
		}
	}
	return TreeListing{CommitSHA: commitSHA, Entries: entries, Truncated: truncated}, nil
}

func (l *Local) addLastCommits(ctx context.Context, dir, commitSHA, treePath string, entries []TreeEntry) error {
	args := []string{"last-modified", "--show-trees", "-z"}
	if treePath == "" {
		args = append(args, "--max-depth=0", commitSHA)
	} else {
		args = append(args, "--max-depth=1", commitSHA, "--", ":(top,literal)"+treePath)
	}

	out, err := l.runGitBytes(ctx, dir, nil, nil, args...)
	if err != nil {
		return fmt.Errorf("list last-modified commits: %w", err)
	}

	byPath, err := parseLastModified(out)
	if err != nil {
		return err
	}

	var in strings.Builder
	seen := make(map[string]bool)
	entrySHAs := make([]string, len(entries))
	for i, entry := range entries {
		sha, ok := byPath[entry.Path]
		if !ok {
			return fmt.Errorf("last-modified output missing path %q", entry.Path)
		}

		entrySHAs[i] = sha
		if !seen[sha] {
			in.WriteString(sha)
			in.WriteByte('\n')
			seen[sha] = true
		}
	}

	out, err = l.runGitBytes(ctx, dir, nil, strings.NewReader(in.String()),
		"log", "--no-walk=unsorted", "--stdin", "-z", "--format=%H%x00%s%x00%cI",
	)
	if err != nil {
		return fmt.Errorf("read last-modified commits: %w", err)
	}

	commits, err := parseCommitSummaries(out)
	if err != nil {
		return err
	}

	for i, sha := range entrySHAs {
		commit, ok := commits[sha]
		if !ok {
			return fmt.Errorf("commit metadata missing for %s", sha)
		}
		entries[i].LastCommit = &commit
	}

	return nil
}

// streams an uncompressed tar archive of the repo tree,
// the tar entries carries LFS pointers files as-is, smudging is a service concern!
func (l *Local) ArchiveTar(ctx context.Context, storagePath, rev string, out io.Writer) (string, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return "", err
	}

	// only the resolution step gets the short timeout
	commitSHA, err := l.ResolveCommit(ctx, storagePath, rev)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "archive", "--format=tar", "--end-of-options", commitSHA)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git archive: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return commitSHA, nil
}

func (l *Local) StatBlob(ctx context.Context, storagePath, rev, treePath string) (BlobInfo, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return BlobInfo{}, err
	}

	treePath, err = normalizeTreePath(treePath)
	if err != nil {
		return BlobInfo{}, err
	}
	if treePath == "" {
		// the root is a tree by definition (and its not a blob)
		return BlobInfo{}, fmt.Errorf("%w: %q", ErrNotABlob, treePath)
	}

	commitSHA, err := l.ResolveCommit(ctx, storagePath, rev)
	if err != nil {
		return BlobInfo{}, err
	}

	blobSHA, err := l.revParse(ctx, dir, commitSHA+":"+treePath)
	if err != nil {
		return BlobInfo{}, fmt.Errorf("%w: %q", ErrPathNotFound, treePath)
	}

	out, err := l.runGit(ctx, dir, nil, strings.NewReader(blobSHA+"\n"), "cat-file", "--batch-check")
	if err != nil {
		return BlobInfo{}, err
	}

	// output shape: "<sha> <type> <size>"
	fields := strings.Fields(out)
	if len(fields) != 3 {
		return BlobInfo{}, fmt.Errorf("malformed batch-check output: %q", out)
	}
	if fields[1] != "blob" {
		return BlobInfo{}, fmt.Errorf("%w: %q is a %s", ErrNotABlob, treePath, fields[1])
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return BlobInfo{}, fmt.Errorf("malformed blob size %q: %w", fields[2], err)
	}

	return BlobInfo{CommitSHA: commitSHA, BlobSHA: blobSHA, Size: size}, nil
}

func (l *Local) ReadBlob(ctx context.Context, storagePath, blobSHA string, out io.Writer) error {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return err
	}

	if !isHexSHA(blobSHA) {
		return fmt.Errorf("%w: %q", ErrInvalidRev, blobSHA)
	}

	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "cat-file", "blob", blobSHA)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cat-file blob: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (l *Local) ResolveCommit(ctx context.Context, storagePath, rev string) (string, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return "", err
	}

	rev, err = normalizeRev(rev)
	if err != nil {
		return "", err
	}

	// ^{commit} forces the object to exist and peel to a commit
	commitSHA, err := l.revParse(ctx, dir, rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrRevNotFound, rev)
	}
	return commitSHA, nil
}

func (l *Local) GetCommit(ctx context.Context, storagePath, sha string) (CommitDetails, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return CommitDetails{}, err
	}
	if !isHexSHA(sha) {
		return CommitDetails{}, fmt.Errorf("%w: %q", ErrInvalidRev, sha)
	}

	commitSHA, err := l.revParse(ctx, dir, sha+"^{commit}")
	if err != nil {
		return CommitDetails{}, fmt.Errorf("%w: %s", ErrRevNotFound, sha)
	}

	out, err := l.runGitBytes(ctx, dir, nil, nil,
		"log", "--no-walk", "-z", "--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%cI%x00%B", "--end-of-options", commitSHA,
	)
	if err != nil {
		return CommitDetails{}, fmt.Errorf("read commit details: %w", err)
	}

	details, err := parseCommitDetails(out)
	if err != nil {
		return CommitDetails{}, err
	}
	if details.SHA != commitSHA {
		return CommitDetails{}, fmt.Errorf("commit metadata mismatch: got %s, want %s", details.SHA, commitSHA)
	}
	return details, nil
}

func (l *Local) WriteBlob(ctx context.Context, storagePath string, r io.Reader) (string, int64, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return "", 0, err
	}

	counter := &countingReader{r: r}
	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "hash-object", "-w", "--stdin")
	cmd.Stdin = counter

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("hash-object: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(out.String()), counter.n, nil
}

func (l *Local) GC(ctx context.Context, storagePath string) error {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, gcTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "gc", "--quiet")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git gc: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runGit executes one short-lived git command with the repo as context,
// applying the standard timeout, and returns its trimmed stdout
func (l *Local) runGit(ctx context.Context, dir string, env []string, stdin io.Reader, args ...string) (string, error) {
	out, err := l.runGitBytes(ctx, dir, env, stdin, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// runGitBytes is the raw-output variant for NUL-delimited git protocols
func (l *Local) runGitBytes(ctx context.Context, dir string, env []string, stdin io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, l.gitPath, append([]string{"-C", dir}, args...)...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdin = stdin
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

// revParse resolves a rev expression to an object id, failing when the object does not exist
func (l *Local) revParse(ctx context.Context, dir, spec string) (string, error) {
	return l.runGit(ctx, dir, nil, nil, "rev-parse", "--verify", "--end-of-options", spec)
}

func parseLsTree(out []byte, treePath string) ([]TreeEntry, bool, error) {
	var entries []TreeEntry
	// each record has a shape like "<mode> <type> <sha> <size>\t<name>"
	for record := range bytes.SplitSeq(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		if len(entries) == maxTreeEntries {
			return entries, true, nil
		}

		// header never contains a tab, but a filename can, so cut at the first one
		header, name, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			return nil, false, fmt.Errorf("malformed ls-tree record: %q", record)
		}
		fields := strings.Fields(string(header))
		if len(fields) != 4 {
			return nil, false, fmt.Errorf("malformed ls-tree header: %q", header)
		}

		size := int64(-1)
		// if size is "-" -> its non-blob item -> size = -1
		if fields[3] != "-" {
			parsed, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil {
				return nil, false, fmt.Errorf("malformed ls-tree size %q: %w", fields[3], err)
			}
			size = parsed
		}

		entries = append(entries, TreeEntry{
			Mode: fields[0],
			Type: fields[1],
			SHA:  fields[2],
			Size: size,
			Path: path.Join(treePath, string(name)),
		})
	}
	return entries, false, nil
}

func parseLastModified(out []byte) (map[string]string, error) {
	commits := make(map[string]string)
	for record := range bytes.SplitSeq(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}

		sha, filePath, ok := bytes.Cut(record, []byte{'\t'})
		if !ok || !isHexSHA(string(sha)) || len(filePath) == 0 {
			return nil, fmt.Errorf("malformed last-modified record: %q", record)
		}

		commits[string(filePath)] = string(sha)
	}

	return commits, nil
}

func parseCommitSummaries(out []byte) (map[string]CommitSummary, error) {
	fields := bytes.Split(out, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}

	if len(fields)%3 != 0 {
		return nil, fmt.Errorf("malformed git log output: got %d fields", len(fields))
	}

	commits := make(map[string]CommitSummary, len(fields)/3)
	for i := 0; i < len(fields); i += 3 {
		sha := string(fields[i])
		if !isHexSHA(sha) {
			return nil, fmt.Errorf("malformed commit sha %q", sha)
		}

		committedAt, err := time.Parse(time.RFC3339, string(fields[i+2]))
		if err != nil {
			return nil, fmt.Errorf("malformed commit time %q: %w", fields[i+2], err)
		}

		commits[sha] = CommitSummary{
			SHA:         sha,
			Message:     string(fields[i+1]),
			CommittedAt: committedAt.UTC(),
		}
	}

	return commits, nil
}

func parseCommitDetails(out []byte) (CommitDetails, error) {
	fields := bytes.Split(out, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields) != 7 {
		return CommitDetails{}, fmt.Errorf("malformed git log output: got %d fields", len(fields))
	}

	sha := string(fields[0])
	if !isHexSHA(sha) {
		return CommitDetails{}, fmt.Errorf("malformed commit sha %q", sha)
	}

	parents := make([]string, 0)
	for parent := range strings.FieldsSeq(string(fields[1])) {
		if !isHexSHA(parent) {
			return CommitDetails{}, fmt.Errorf("malformed parent sha %q", parent)
		}
		parents = append(parents, parent)
	}

	authoredAt, err := time.Parse(time.RFC3339, string(fields[4]))
	if err != nil {
		return CommitDetails{}, fmt.Errorf("malformed author time %q: %w", fields[4], err)
	}

	committedAt, err := time.Parse(time.RFC3339, string(fields[5]))
	if err != nil {
		return CommitDetails{}, fmt.Errorf("malformed commit time %q: %w", fields[5], err)
	}

	return CommitDetails{
		SHA:     sha,
		Parents: parents,
		Message: strings.TrimSuffix(string(fields[6]), "\n"),
		Author: Identity{
			Name:  string(fields[2]),
			Email: string(fields[3]),
		},
		AuthoredAt:  authoredAt.UTC(),
		CommittedAt: committedAt.UTC(),
	}, nil
}

// normalizeRev validates an untrusted revision expression; empty means HEAD
func normalizeRev(rev string) (string, error) {
	if rev == "" {
		return "HEAD", nil
	}
	if strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("%w: %q", ErrInvalidRev, rev)
	}
	for _, r := range rev {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: control character", ErrInvalidRev)
		}
	}
	return rev, nil
}

// normalizeTreePath validates an untrusted tree path and normalizes it
func normalizeTreePath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: contains NUL", ErrInvalidPath)
	}
	return path.Clean("/" + p)[1:], nil
}

func (l *Local) lockRepo(storagePath string) func() {
	l.mu.Lock()
	m, ok := l.locks[storagePath]
	if !ok {
		m = &sync.Mutex{}
		l.locks[storagePath] = m
	}
	l.mu.Unlock()

	m.Lock()
	return m.Unlock
}

// compares before and after refs, and returns structured list of RefChange,
// public, so its testable
func DiffRefs(before, after map[string]string) []RefChange {
	var changes []RefChange

	// compare before refs with after
	for ref, oldSHA := range before {
		switch newSHA, ok := after[ref]; {
		case !ok: // if after is missing -> it was deleted, and new sha = 0
			changes = append(changes, RefChange{Ref: ref, OldSHA: oldSHA, NewSHA: zeroSHA})
		case newSHA != oldSHA: // if after is different -> it was just updated
			changes = append(changes, RefChange{Ref: ref, OldSHA: oldSHA, NewSHA: newSHA})
		}
	}

	// compare after refs with before
	for ref, newSHA := range after {
		// if before is missing -> ref was created, and old sha = 0
		if _, ok := before[ref]; !ok {
			changes = append(changes, RefChange{Ref: ref, OldSHA: zeroSHA, NewSHA: newSHA})
		}
	}

	return changes
}

// resolve maps a stored relative path to an absolute dir under the root
// refuses anything that escapes it
func (l *Local) resolve(storagePath string) (string, error) {
	full := filepath.Join(l.root, filepath.Clean("/"+storagePath))
	if full != l.root && !strings.HasPrefix(full, l.root+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid storage path: %s", storagePath)
	}
	return full, nil
}

func isHexSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isLFSOID(oid string) bool {
	if len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isAttributesPath(treePath string) bool {
	return treePath == ".gitattributes" || strings.HasSuffix(treePath, "/.gitattributes")
}

// just to keep track how much bytes were streamed
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
