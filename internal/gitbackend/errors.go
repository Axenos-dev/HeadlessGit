package gitbackend

import "errors"

var (
	ErrInvalidRev   = errors.New("invalid revision")
	ErrInvalidPath  = errors.New("invalid tree path")
	ErrRevNotFound  = errors.New("revision not found")
	ErrPathNotFound = errors.New("path not found in tree")
	ErrNotABlob     = errors.New("path is not a blob")

	// commit creation
	ErrInvalidBranch   = errors.New("invalid branch name")
	ErrInvalidOps      = errors.New("invalid commit operations")
	ErrUnknownBlob     = errors.New("blob not found in repository")
	ErrHeadMismatch    = errors.New("branch head mismatch")
	ErrNothingToCommit = errors.New("nothing to commit")
	ErrLFSRequired     = errors.New("path is lfs-tracked but no clean filter is available")
)
