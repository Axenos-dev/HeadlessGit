package lfs

import "errors"

var (
	ErrInvalidOID           = errors.New("lfs: invalid object id")
	ErrObjectNotFound       = errors.New("lfs: object not found")
	ErrObjectMismatch       = errors.New("lfs: object content does not match oid/size")
	ErrUnsupportedOperation = errors.New("lfs: unsupported batch operation")
	ErrInvalidUpload        = errors.New("lfs: invalid upload authorization")
	ErrUploadExpired        = errors.New("lfs: upload authorization expired")
)
