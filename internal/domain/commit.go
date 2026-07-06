package domain

type CommitIdentity struct {
	Name  string
	Email string
}

type CommitFileOp struct {
	Delete     bool
	Path       string
	BlobSHA    string
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
