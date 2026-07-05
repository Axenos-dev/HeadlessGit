package archive

import (
	"strconv"
	"strings"
)

// pointer file is a small key-value text blob
// and it cant be bigger than this
const lfsPointerMaxSize = 1024
const lfsVersionLine = "version https://git-lfs.github.com/spec/v1"

type lfsPointer struct {
	oid  string
	size int64
}

func parseLFSPointer(data []byte) (lfsPointer, bool) {
	if len(data) == 0 || len(data) > lfsPointerMaxSize {
		return lfsPointer{}, false
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) < 3 || lines[0] != lfsVersionLine {
		return lfsPointer{}, false
	}

	ptr := lfsPointer{size: -1}
	for _, line := range lines[1:] {
		if oid, ok := strings.CutPrefix(line, "oid sha256:"); ok {
			ptr.oid = oid
		} else if raw, ok := strings.CutPrefix(line, "size "); ok {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return lfsPointer{}, false
			}
			ptr.size = n
		}
	}

	if !validOID(ptr.oid) || ptr.size < 0 {
		return lfsPointer{}, false
	}
	return ptr, true
}

func validOID(oid string) bool {
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
