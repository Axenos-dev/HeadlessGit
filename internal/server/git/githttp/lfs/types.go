package lfs

import (
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

// git-lfs batch API types (application/vnd.git-lfs+json)

type batchRequest struct {
	Operation string        `json:"operation"`
	Transfers []string      `json:"transfers,omitempty"`
	Objects   []pointerJSON `json:"objects"`
}

type pointerJSON struct {
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

type batchResponse struct {
	Transfer string       `json:"transfer,omitempty"`
	Objects  []objectJSON `json:"objects"`
}

type objectJSON struct {
	OID     string                `json:"oid"`
	Size    int64                 `json:"size"`
	Actions map[string]actionJSON `json:"actions,omitempty"`
	Error   *objectErrorJSON      `json:"error,omitempty"`
}

type actionJSON struct {
	Href      string            `json:"href"`
	Header    map[string]string `json:"header,omitempty"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

type objectErrorJSON struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type verifyRequest struct {
	OID  string `json:"oid"`
	Size int64  `json:"size"`
}

type errorResponse struct {
	Message string `json:"message"`
}

func toObjectsJSON(results []domain.LFSObjectResponse) []objectJSON {
	out := make([]objectJSON, len(results))
	for i, res := range results {
		obj := objectJSON{OID: res.OID, Size: res.Size}

		if res.Error != nil {
			obj.Error = &objectErrorJSON{Code: res.Error.Code, Message: res.Error.Message}
		}

		if len(res.Actions) > 0 {
			obj.Actions = make(map[string]actionJSON, len(res.Actions))
			for name, a := range res.Actions {
				action := actionJSON{Href: a.Href, Header: a.Header}
				if !a.ExpiresAt.IsZero() {
					t := a.ExpiresAt.UTC()
					action.ExpiresAt = &t
				}
				obj.Actions[name] = action
			}
		}

		out[i] = obj
	}
	return out
}
