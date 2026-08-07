package domain

import "time"

type CommitIdentity struct {
	Name  string
	Email string
}

type CommitDetails struct {
	SHA         string
	Parents     []string
	Message     string
	Author      CommitIdentity
	AuthoredAt  time.Time
	CommittedAt time.Time
}

type CommitFileLfsObject struct {
	OID  string
	Size int64
}

type CommitFileOp struct {
	Delete     bool
	MoveFrom   string
	Path       string
	BlobSHA    *string
	Lfs        *CommitFileLfsObject
	Executable bool
}

type CommitRequest struct {
	Branch          string
	Message         string
	Author          CommitIdentity
	ExpectedHeadSHA string // pins the commit to an exact branch state
	PusherID        int64  // optionally attributes the push event to an account
	Operations      []CommitFileOp
}

type CommitResult struct {
	Branch    string
	CommitSHA string
	Before    string // the branch head the commit was built on; all-zero for a new branch
}
