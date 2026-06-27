package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSSHKey      = errors.New("invalid ssh key")
	ErrSSHKeyNotFound     = errors.New("ssh key not found")
	ErrTokenNotFound      = errors.New("token not found")
)
