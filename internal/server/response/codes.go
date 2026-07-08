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
	CodeRefNotFound        = "ref_not_found"
	CodePathNotFound       = "path_not_found"
	CodeLFSObjectNotFound  = "lfs_object_not_found"

	CodeHeadMismatch    = "head_mismatch"
	CodeUnknownBlob     = "unknown_blob"
	CodeNothingToCommit = "nothing_to_commit"

	CodePathBlocked      = "path_blocked"
	CodePathPolicyExists = "path_policy_exists"
	CodeUserExists       = "user_exists"
	CodeRepositoryExists = "repository_exists"
	CodeWebhookExists    = "webhook_exists"
)
