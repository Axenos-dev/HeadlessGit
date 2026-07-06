package domain

type BlobRequest struct {
	Repository Repository
	CommitSHA  string
	BlobSHA    string
	Path       string
	Size       int64  // exact byte count of what will be streamed
	LFSOID     string // non-empty when the blob is an LFS pointer being smudged
}
