package domain

import "time"

type LFSOperation string

const (
	LFSOperationUpload   LFSOperation = "upload"
	LFSOperationDownload LFSOperation = "download"
)

type LFSPointer struct {
	OID  string
	Size int64
}

type LFSAction struct {
	Href      string
	Header    map[string]string
	ExpiresAt time.Time
}

type LFSObjectError struct {
	Code    int
	Message string
}

type LFSObjectResponse struct {
	OID     string
	Size    int64
	Actions map[string]LFSAction
	Error   *LFSObjectError
}
