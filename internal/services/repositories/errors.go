package repositories

import "errors"

var (
	ErrRepositoryNotFound    = errors.New("repository not found")
	ErrInvalidRepositoryName = errors.New("invalid repository name")
	ErrInvalidVisibility     = errors.New("invalid visibility")
)
