package gitbackend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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

func (l *Local) Pack(ctx context.Context, storagePath string, svc Service, stateless bool, stdin io.Reader, stdout, stderr io.Writer) error {
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

// resolve maps a stored relative path to an absolute dir under the root
// refuses anything that escapes it
func (l *Local) resolve(storagePath string) (string, error) {
	full := filepath.Join(l.root, filepath.Clean("/"+storagePath))
	if full != l.root && !strings.HasPrefix(full, l.root+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid storage path: %s", storagePath)
	}
	return full, nil
}
