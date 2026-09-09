package githttp

import "github.com/Axenos-dev/HeadlessGit/internal/domain"

type uploadedObject struct {
	Kind domain.UploadKind `json:"kind"`
	SHA  string            `json:"sha,omitempty"`
	OID  string            `json:"oid,omitempty"`
	Size int64             `json:"size"`
}
