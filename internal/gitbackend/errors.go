package gitbackend

import "errors"

var (
	ErrInvalidRev   = errors.New("invalid revision")
	ErrInvalidPath  = errors.New("invalid tree path")
	ErrRevNotFound  = errors.New("revision not found")
	ErrPathNotFound = errors.New("path not found in tree")
	ErrNotABlob     = errors.New("path is not a blob")
)
