package response

const (
	CodeInternalError  = "internal_error"
	CodeInvalidRequest = "invalid_request"
	CodeUnauthorized   = "unauthorized"
	CodeForbidden      = "forbidden"

	CodeRepositoryNotFound = "repository_not_found"
	CodeUserNotFound       = "user_not_found"
	CodeSSHKeyNotFound     = "ssh_key_not_found"
	CodeTokenNotFound      = "token_not_found"
)
