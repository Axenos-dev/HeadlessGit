package permissions

import "errors"

var (
	ErrAccessDenied = errors.New("access denied")
	ErrInvalidRole  = errors.New("invalid role")
)
