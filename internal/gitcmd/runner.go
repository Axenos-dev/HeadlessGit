package gitcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Runner struct {
	root    string
	gitPath string
	timeout time.Duration
}

func NewRunner(root string) (*Runner, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git binary not found: %w", err)
	}

	return &Runner{
		root:    absRoot,
		gitPath: gitPath,
		timeout: 30 * time.Second,
	}, nil
}

func (r *Runner) InitBareRepository(ctx context.Context, storagePath string) error {
	dir, err := r.resolve(storagePath)
	if err != nil {
		return err
	}

	// create folder for repo
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// initiate bare repo in the folder
	cmd := exec.CommandContext(ctx, r.gitPath, "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init --bare: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// just deletes the folder with bare repo
func (r *Runner) Remove(ctx context.Context, storagePath string) error {
	dir, err := r.resolve(storagePath)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// resolve maps a stored relative path to an absolute dir under the root and
// refuses anything that escapes it
func (r *Runner) resolve(storagePath string) (string, error) {
	full := filepath.Join(r.root, filepath.Clean("/"+storagePath))
	if full != r.root && !strings.HasPrefix(full, r.root+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid storage path: %s", storagePath)
	}
	return full, nil
}
