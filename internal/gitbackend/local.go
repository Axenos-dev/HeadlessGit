package gitbackend

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
