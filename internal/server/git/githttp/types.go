package githttp

type uploadedObject struct {
	BlobSHA string `json:"blobSha"`
	Size    int64  `json:"size"`
}
