package gitbackend

// the all-zero object id git uses to denote a missing ref
// its zero in before, if it was created after
// or its zero after, if it was deleted before
const zeroSHA = "0000000000000000000000000000000000000000"

// hard cap on entries returned per directory level
const maxTreeEntries = 10_000

// hard cap on operations per commit
const maxCommitOps = 1000

type Service int

const (
	UploadPack  Service = iota // fetch / clone
	ReceivePack                // push
)

// returns the command name of the service
func (s Service) Name() string {
	if s == ReceivePack {
		return "git-receive-pack"
	}
	return "git-upload-pack"
}

type RefChange struct {
	Ref    string
	OldSHA string
	NewSHA string
}

type TreeEntry struct {
	Mode string // "100644", "100755", "040000", "120000", "160000"
	Type string // "blob" | "tree" | "commit"
	SHA  string
	Size int64  // object size in bytes, -1 where git reports none (trees, submodules)
	Path string // full path from the repo root
}

type TreeListing struct {
	CommitSHA string // the exact commit the listing is a snapshot of
	Entries   []TreeEntry
	Truncated bool
}

type BlobInfo struct {
	CommitSHA string
	BlobSHA   string
	Size      int64
}

type CommitOp struct {
	Delete  bool
	Path    string
	BlobSHA string // puts only; must exist as a blob in this repo's odb
	Mode    string // puts only: "100644" (default) or "100755"
}

type Identity struct {
	Name  string
	Email string
}

type CommitSpec struct {
	Branch string
	// ExpectedOld pins the commit to an exact branch state
	ExpectedOld string
	Author      Identity
	Committer   Identity // defaults to Author when empty
	Message     string
}

// write-side mirror of archive.SmudgeFunc
type CleanFunc func(path, blobSHA string, size int64) (string, error)
