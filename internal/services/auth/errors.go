package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSSHKey      = errors.New("invalid ssh key")
)
