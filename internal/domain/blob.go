package domain

type FileInfo struct {
	BlobSHA string
	Size    int64 // logical file size
}

type FileRequest struct {
	Repository Repository
	BlobSHA    string
	Size       int64  // exact byte count of what will be streamed
	LFSOID     string // non-empty when the blob is an LFS pointer being smudged
}
