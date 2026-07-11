package repositories

import "errors"

var (
	ErrRepositoryNotFound    = errors.New("repository not found")
	ErrRepositoryExists      = errors.New("repository already exists")
	ErrInvalidRepositoryName = errors.New("invalid repository name")
	ErrInvalidVisibility     = errors.New("invalid visibility")

	ErrRefNotFound  = errors.New("ref not found")
	ErrPathNotFound = errors.New("path not found")
	ErrInvalidRef   = errors.New("invalid ref")
	ErrInvalidPath  = errors.New("invalid path")

	ErrUnsupportedFormat    = errors.New("unsupported archive format")
	ErrInvalidArchivePrefix = errors.New("invalid archive prefix")
	ErrLFSNotEnabled        = errors.New("lfs is not enabled")

	ErrNotAFile          = errors.New("path is not a file")
	ErrLFSObjectNotFound = errors.New("lfs object not found")

	// api commits
	ErrInvalidBranch    = errors.New("invalid branch name")
	ErrInvalidCommitOps = errors.New("invalid commit operations")
	ErrHeadMismatch     = errors.New("branch head mismatch")
	ErrUnknownBlob      = errors.New("blob not found in repository")
	ErrNothingToCommit  = errors.New("nothing to commit")

	// path policies
	ErrInvalidPathPattern = errors.New("invalid path pattern")
	ErrPathPolicyExists   = errors.New("path policy already exists")
	ErrPathBlocked        = errors.New("path blocked by policy")
)
