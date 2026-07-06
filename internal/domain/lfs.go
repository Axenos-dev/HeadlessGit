package domain

import (
	"strconv"
	"strings"
	"time"
)

type LFSOperation string

const (
	LFSOperationUpload   LFSOperation = "upload"
	LFSOperationDownload LFSOperation = "download"
)

const LFSPointerMaxSize = 1024
const lfsVersionLine = "version https://git-lfs.github.com/spec/v1"

type LFSPointer struct {
	OID  string
	Size int64
}

type LFSAction struct {
	Href      string
	Header    map[string]string
	ExpiresAt time.Time
}

type LFSObjectError struct {
	Code    int
	Message string
}

type LFSObjectResponse struct {
	OID     string
	Size    int64
	Actions map[string]LFSAction
	Error   *LFSObjectError
}

func ParseLFSPointer(data []byte) (LFSPointer, bool) {
	if len(data) == 0 || len(data) > LFSPointerMaxSize {
		return LFSPointer{}, false
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) < 3 || lines[0] != lfsVersionLine {
		return LFSPointer{}, false
	}

	ptr := LFSPointer{Size: -1}
	for _, line := range lines[1:] {
		if oid, ok := strings.CutPrefix(line, "oid sha256:"); ok {
			ptr.OID = oid
		} else if raw, ok := strings.CutPrefix(line, "size "); ok {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return LFSPointer{}, false
			}
			ptr.Size = n
		}
	}

	if !validLFSOID(ptr.OID) || ptr.Size < 0 {
		return LFSPointer{}, false
	}
	return ptr, true
}

// enforces a 64 char lowercase hex sha256
func validLFSOID(oid string) bool {
	if len(oid) != 64 {
		return false
	}
	for _, c := range oid {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
