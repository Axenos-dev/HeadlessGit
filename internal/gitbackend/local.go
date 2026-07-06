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

type Service int

const (
	UploadPack  Service = iota // fetch / clone
	ReceivePack                // push
)

// repacking a large repo can take a while
const gcTimeout = 30 * time.Minute

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

	treeish := commitSHA
	if treePath != "" {
		treeish += ":" + treePath
	}

	out, err := l.runGit(ctx, dir, nil, nil, "ls-tree", "--long", "-z", "--end-of-options", treeish)
	if err != nil {
		// the rev already resolved, so this is a missing path or a non-directory
		return TreeListing{}, fmt.Errorf("%w: %q", ErrPathNotFound, treePath)
	}

	entries, truncated, err := parseLsTree([]byte(out), treePath)
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

// creates a commit on a branch from already-uploaded blobs
func (l *Local) ApplyCommit(ctx context.Context, storagePath string, spec CommitSpec, ops []CommitOp, clean CleanFunc) (RefChange, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return RefChange{}, err
	}

	spec, err = l.validateCommitSpec(ctx, dir, spec)
	if err != nil {
		return RefChange{}, err
	}
	ops, err = validateCommitOps(ops)
	if err != nil {
		return RefChange{}, err
	}

	// resolve the current branch head
	ref := "refs/heads/" + spec.Branch
	oldSHA, err := l.revParse(ctx, dir, ref)
	unborn := err != nil
	switch {
	case unborn && spec.ExpectedOld != zeroSHA:
		return RefChange{}, fmt.Errorf("%w: branch %s", ErrRevNotFound, spec.Branch)
	case !unborn && spec.ExpectedOld == zeroSHA:
		return RefChange{}, fmt.Errorf("%w: branch %s already exists", ErrHeadMismatch, spec.Branch)
	case !unborn && spec.ExpectedOld != "" && spec.ExpectedOld != oldSHA:
		return RefChange{}, fmt.Errorf("%w: expected %s, head is %s", ErrHeadMismatch, spec.ExpectedOld, oldSHA)
	}
	if unborn {
		oldSHA = zeroSHA
	}

	// one batch-check verifies every referenced blob (and captures sizes for
	// the clean filter) plus the existence of every delete target
	sizes, err := l.verifyCommitInputs(ctx, dir, oldSHA, unborn, ops)
	if err != nil {
		return RefChange{}, err
	}

	// private index file: commits never touch the repo's real index (bare
	// repos have none) and concurrent commits cannot see each other
	idx, err := os.CreateTemp(dir, "headlessgit-index-*")
	if err != nil {
		return RefChange{}, fmt.Errorf("create temp index: %w", err)
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := []string{"GIT_INDEX_FILE=" + idx.Name()}

	if unborn {
		if _, err := l.runGit(ctx, dir, env, nil, "read-tree", "--empty"); err != nil {
			return RefChange{}, err
		}
	} else {
		if _, err := l.runGit(ctx, dir, env, nil, "read-tree", oldSHA); err != nil {
			return RefChange{}, err
		}
	}

	// .gitattributes changes land first, so lfs tracking added in this very
	// commit already applies to the files committed alongside it
	attrOps, fileOps := splitAttrOps(ops)
	if err := l.updateIndex(ctx, dir, env, attrOps); err != nil {
		return RefChange{}, err
	}

	fileOps, err = l.cleanLFSTracked(ctx, dir, env, fileOps, sizes, clean)
	if err != nil {
		return RefChange{}, err
	}
	if err := l.updateIndex(ctx, dir, env, fileOps); err != nil {
		return RefChange{}, err
	}

	treeSHA, err := l.runGit(ctx, dir, env, nil, "write-tree")
	if err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrInvalidOps, err)
	}
	if !unborn {
		oldTree, err := l.revParse(ctx, dir, oldSHA+"^{tree}")
		if err != nil {
			return RefChange{}, err
		}
		if treeSHA == oldTree {
			return RefChange{}, ErrNothingToCommit
		}
	}

	commitEnv := []string{
		"GIT_AUTHOR_NAME=" + spec.Author.Name,
		"GIT_AUTHOR_EMAIL=" + spec.Author.Email,
		"GIT_COMMITTER_NAME=" + spec.Committer.Name,
		"GIT_COMMITTER_EMAIL=" + spec.Committer.Email,
	}
	args := []string{"commit-tree", treeSHA}
	if !unborn {
		args = append(args, "-p", oldSHA)
	}
	args = append(args, "-m", spec.Message)
	newSHA, err := l.runGit(ctx, dir, commitEnv, nil, args...)
	if err != nil {
		return RefChange{}, err
	}

	// update-ref only moves the branch if it still points at oldSHA
	if _, err := l.runGit(ctx, dir, nil, nil, "update-ref", ref, newSHA, oldSHA); err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrHeadMismatch, err)
	}

	return RefChange{Ref: ref, OldSHA: oldSHA, NewSHA: newSHA}, nil
}

// validateCommitSpec checks the branch name and identities
func (l *Local) validateCommitSpec(ctx context.Context, dir string, spec CommitSpec) (CommitSpec, error) {
	if spec.Branch == "" || strings.HasPrefix(spec.Branch, "-") {
		return spec, fmt.Errorf("%w: %q", ErrInvalidBranch, spec.Branch)
	}
	for _, r := range spec.Branch {
		if r < 0x20 || r == 0x7f {
			return spec, fmt.Errorf("%w: control character", ErrInvalidBranch)
		}
	}
	// git itself is the authority on ref name rules
	if _, err := l.runGit(ctx, dir, nil, nil, "check-ref-format", "--branch", spec.Branch); err != nil {
		return spec, fmt.Errorf("%w: %q", ErrInvalidBranch, spec.Branch)
	}

	if spec.ExpectedOld != "" && !isHexSHA(spec.ExpectedOld) {
		return spec, fmt.Errorf("%w: expected old %q", ErrInvalidRev, spec.ExpectedOld)
	}
	if spec.Author.Name == "" || spec.Author.Email == "" {
		return spec, fmt.Errorf("%w: author name and email are required", ErrInvalidOps)
	}
	if spec.Committer.Name == "" {
		spec.Committer = spec.Author
	}
	if spec.Message == "" {
		return spec, fmt.Errorf("%w: message is required", ErrInvalidOps)
	}
	return spec, nil
}

// normalizes paths and enforces the op rules
func validateCommitOps(ops []CommitOp) ([]CommitOp, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: no operations", ErrInvalidOps)
	}
	if len(ops) > maxCommitOps {
		return nil, fmt.Errorf("%w: more than %d operations", ErrInvalidOps, maxCommitOps)
	}

	out := make([]CommitOp, len(ops))
	seen := make(map[string]bool, len(ops))
	for i, op := range ops {
		p, err := normalizeTreePath(op.Path)
		if err != nil || p == "" {
			return nil, fmt.Errorf("%w: path %q", ErrInvalidOps, op.Path)
		}
		// the batch-check and check-attr line protocols cannot carry these
		for _, r := range p {
			if r < 0x20 || r == 0x7f {
				return nil, fmt.Errorf("%w: control character in path", ErrInvalidOps)
			}
		}
		if seen[p] {
			return nil, fmt.Errorf("%w: duplicate path %q", ErrInvalidOps, p)
		}
		seen[p] = true

		op.Path = p
		if !op.Delete {
			if !isHexSHA(op.BlobSHA) {
				return nil, fmt.Errorf("%w: blob sha %q", ErrInvalidOps, op.BlobSHA)
			}
			switch op.Mode {
			case "":
				op.Mode = "100644"
			case "100644", "100755":
			default:
				return nil, fmt.Errorf("%w: mode %q", ErrInvalidOps, op.Mode)
			}
		}
		out[i] = op
	}
	return out, nil
}

// runs one cat-file --batch-check over every put blob sha (returning their sizes)
// and every delete target
// so unknown blobs and missing delete paths fail
func (l *Local) verifyCommitInputs(ctx context.Context, dir, oldSHA string, unborn bool, ops []CommitOp) (map[string]int64, error) {
	var in strings.Builder
	type query struct {
		op    CommitOp
		isPut bool
	}
	var queries []query
	for _, op := range ops {
		if op.Delete {
			if unborn {
				return nil, fmt.Errorf("%w: %q", ErrPathNotFound, op.Path)
			}
			in.WriteString(oldSHA + ":" + op.Path + "\n")
		} else {
			in.WriteString(op.BlobSHA + "\n")
		}
		queries = append(queries, query{op: op, isPut: !op.Delete})
	}

	out, err := l.runGit(ctx, dir, nil, strings.NewReader(in.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(out, "\n")
	if len(lines) != len(queries) {
		return nil, fmt.Errorf("unexpected batch-check output: %d lines for %d queries", len(lines), len(queries))
	}

	sizes := make(map[string]int64)
	for i, line := range lines {
		q := queries[i]
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 2 && fields[len(fields)-1] == "missing":
			if q.isPut {
				return nil, fmt.Errorf("%w: %s", ErrUnknownBlob, q.op.BlobSHA)
			}
			return nil, fmt.Errorf("%w: %q", ErrPathNotFound, q.op.Path)
		case len(fields) == 3 && fields[1] == "blob":
			size, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed batch-check size %q: %w", fields[2], err)
			}
			if q.isPut {
				sizes[q.op.BlobSHA] = size
			}
		case len(fields) == 3:
			// a delete target resolving to a tree or a put sha naming a non-blob
			if q.isPut {
				return nil, fmt.Errorf("%w: %s is a %s", ErrUnknownBlob, q.op.BlobSHA, fields[1])
			}
			return nil, fmt.Errorf("%w: %q is a %s", ErrNotABlob, q.op.Path, fields[1])
		default:
			return nil, fmt.Errorf("malformed batch-check line: %q", line)
		}
	}
	return sizes, nil
}

// separates .gitattributes changes from regular file changes
func splitAttrOps(ops []CommitOp) (attr, files []CommitOp) {
	for _, op := range ops {
		if op.Path == ".gitattributes" || strings.HasSuffix(op.Path, "/.gitattributes") {
			attr = append(attr, op)
		} else {
			files = append(files, op)
		}
	}
	return attr, files
}

// asks git which put paths are lfs-tracked,
// and swaps their blobs for the pointer blobs produced by the clean filter
func (l *Local) cleanLFSTracked(ctx context.Context, dir string, env []string, ops []CommitOp, sizes map[string]int64, clean CleanFunc) ([]CommitOp, error) {
	var paths []string
	byPath := make(map[string]int)
	for i, op := range ops {
		if !op.Delete {
			paths = append(paths, op.Path)
			byPath[op.Path] = i
		}
	}
	if len(paths) == 0 {
		return ops, nil
	}

	args := append([]string{"check-attr", "-z", "--cached", "filter", "--"}, paths...)
	out, err := l.runGit(ctx, dir, env, nil, args...)
	if err != nil {
		return nil, err
	}

	// -z output is NUL-separated (path, attr, value) triples
	fields := strings.Split(out, "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		path, value := fields[i], fields[i+2]
		if value != "lfs" {
			continue
		}
		idx, ok := byPath[path]
		if !ok {
			continue
		}
		if clean == nil {
			return nil, fmt.Errorf("%w: %q", ErrLFSRequired, path)
		}

		pointerSHA, err := clean(path, ops[idx].BlobSHA, sizes[ops[idx].BlobSHA])
		if err != nil {
			return nil, fmt.Errorf("lfs clean %q: %w", path, err)
		}
		if !isHexSHA(pointerSHA) {
			return nil, fmt.Errorf("lfs clean %q returned invalid sha %q", path, pointerSHA)
		}
		ops[idx].BlobSHA = pointerSHA
	}
	return ops, nil
}

// applies puts and deletes in one subprocess
func (l *Local) updateIndex(ctx context.Context, dir string, env []string, ops []CommitOp) error {
	if len(ops) == 0 {
		return nil
	}
	var in strings.Builder
	for _, op := range ops {
		if op.Delete {
			in.WriteString("0 " + zeroSHA + "\t" + op.Path + "\x00")
		} else {
			in.WriteString(op.Mode + " " + op.BlobSHA + "\t" + op.Path + "\x00")
		}
	}
	if _, err := l.runGit(ctx, dir, env, strings.NewReader(in.String()), "update-index", "-z", "--index-info"); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidOps, err)
	}
	return nil
}

// runGit executes one short-lived git command with the repo as context,
// applying the standard timeout, and returns its trimmed stdout
func (l *Local) runGit(ctx context.Context, dir string, env []string, stdin io.Reader, args ...string) (string, error) {
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
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
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
