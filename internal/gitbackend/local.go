package gitbackend

import (
	"bufio"
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

type Service int

const (
	UploadPack  Service = iota // fetch / clone
	ReceivePack                // push
)

// returns the command name of the service
func (s Service) Name() string {
	if s == ReceivePack {
		return "git-receive-pack"
	}
	return "git-upload-pack"
}

// local implementation of Git backend
// it runs the git pack protocol against bare repos on the local filesystem
type Local struct {
	root    string
	gitPath string
	timeout time.Duration

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

	return &Local{
		root:    absRoot,
		gitPath: gitPath,
		timeout: 30 * time.Second,
		locks:   make(map[string]*sync.Mutex),
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
	return l.pack(ctx, storagePath, UploadPack, stateless, stdin, stdout, stderr)
}

func (l *Local) ReceivePack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) ([]RefChange, error) {
	// important to lock concurrent pushes
	// as we compare refs before/after for one operation
	unlock := l.lockRepo(storagePath)
	defer unlock()

	// list refs BEFORE the push
	before, beforeErr := l.listRefs(ctx, storagePath)
	// and IGNORE error, as we dont need to block main receive-pack operation

	if err := l.pack(ctx, storagePath, ReceivePack, stateless, stdin, stdout, stderr); err != nil {
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

func (l *Local) pack(ctx context.Context, storagePath string, svc Service, stateless bool, stdin io.Reader, stdout, stderr io.Writer) error {
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

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "for-each-ref", "--format=%(objectname) %(refname)")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("for-each-ref: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	refs := make(map[string]string)
	sc := bufio.NewScanner(&out)
	for sc.Scan() {
		// cut the string by " ", to separate sha from ref
		sha, ref, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		refs[ref] = sha
	}
	return refs, sc.Err()
}

func (l *Local) ListTree(ctx context.Context, storagePath, rev, treePath string) (TreeListing, error) {
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

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	treeish := commitSHA
	if treePath != "" {
		treeish += ":" + treePath
	}

	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "ls-tree", "--long", "-z", "--end-of-options", treeish)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// the rev already resolved, so this is a missing path or a non-directory
		return TreeListing{}, fmt.Errorf("%w: %q", ErrPathNotFound, treePath)
	}

	entries, truncated, err := parseLsTree(out.Bytes(), treePath)
	if err != nil {
		return TreeListing{}, err
	}
	return TreeListing{CommitSHA: commitSHA, Entries: entries, Truncated: truncated}, nil
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

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	blobSHA, err := l.revParse(ctx, dir, commitSHA+":"+treePath)
	if err != nil {
		return BlobInfo{}, fmt.Errorf("%w: %q", ErrPathNotFound, treePath)
	}

	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "cat-file", "--batch-check")
	cmd.Stdin = strings.NewReader(blobSHA + "\n")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return BlobInfo{}, fmt.Errorf("cat-file --batch-check: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// output shape: "<sha> <type> <size>"
	fields := strings.Fields(out.String())
	if len(fields) != 3 {
		return BlobInfo{}, fmt.Errorf("malformed batch-check output: %q", out.String())
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

	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	// ^{commit} forces the object to exist and peel to a commit
	commitSHA, err := l.revParse(ctx, dir, rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrRevNotFound, rev)
	}
	return commitSHA, nil
}

// revParse resolves a rev expression to an object id, failing when the object does not exist
func (l *Local) revParse(ctx context.Context, dir, spec string) (string, error) {
	cmd := exec.CommandContext(ctx, l.gitPath, "-C", dir, "rev-parse", "--verify", "--end-of-options", spec)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rev-parse: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
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
