package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Axenos-dev/HeadlessGit/internal/services/lfs"
)

var (
	_ lfs.ObjectStorage = (*Disk)(nil)
)

// filesystem-backed object store
// implements the services/lfs Storage interface
type Disk struct {
	root string
}

func NewDisk(root string) (*Disk, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve lfs root: %w", err)
	}
	// create root dir if not exists
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create lfs root: %w", err)
	}
	return &Disk{root: abs}, nil
}

func (d *Disk) Stat(_ context.Context, key string) (bool, int64, error) {
	full, err := d.resolve(key)
	if err != nil {
		return false, 0, err
	}
	fi, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return true, fi.Size(), nil
}

func (d *Disk) Get(_ context.Context, key string) (io.ReadCloser, error) {
	full, err := d.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

// writes to a temp file then renames it
func (d *Disk) Put(_ context.Context, key string, _ int64, r io.Reader) error {
	full, err := d.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(full), ".upload-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, full); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (d *Disk) Delete(_ context.Context, key string) error {
	full, err := d.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// maps a storage key to an absolute path under the root,
// and errors if key escapes it
func (d *Disk) resolve(key string) (string, error) {
	full := filepath.Join(d.root, filepath.Clean("/"+key))
	if full != d.root && !strings.HasPrefix(full, d.root+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid object key: %s", key)
	}
	return full, nil
}
