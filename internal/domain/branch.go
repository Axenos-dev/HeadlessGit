package domain

type Branch struct {
	Name      string // short name, without refs/heads/
	CommitSHA string
}

type BranchListing struct {
	DefaultBranch string // the branch HEAD points at, which clones check out; it may not exist yet
	Branches      []Branch
	Truncated     bool
}

type CreateBranchRequest struct {
	Name     string
	SHA      string // the existing commit the branch starts at
	PusherID int64  // optionally attributes the push event to an account
}

type DeleteBranchRequest struct {
	Name        string
	ExpectedSHA string // optionally pins the delete to an exact branch head
	PusherID    int64  // optionally attributes the push event to an account
}
