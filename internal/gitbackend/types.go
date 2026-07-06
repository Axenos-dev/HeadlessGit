package gitbackend

// the all-zero object id git uses to denote a missing ref
// its zero in before, if it was created after
// or its zero after, if it was deleted before
const zeroSHA = "0000000000000000000000000000000000000000"

// hard cap on entries returned per directory level
const maxTreeEntries = 10_000

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
