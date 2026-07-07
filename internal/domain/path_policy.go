package domain

import (
	"strings"
	"time"
)

type PathPolicyKind string

const (
	PathPolicyBlock PathPolicyKind = "block"
	// lfs, size, etc etc later
)

type PathPolicy struct {
	ID           int64
	RepositoryID int64
	Pattern      string
	Kind         PathPolicyKind
	Reason       string // optional, echoed in rejection messages
	CreatedAt    time.Time
}

func NormalizePathPattern(pattern string) (string, bool) {
	p := strings.Trim(pattern, "/")
	if p == "" || p == "." {
		return "", false
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	if strings.ContainsAny(p, "\x00\n") {
		return "", false
	}
	return p, true
}

func PathBlocked(patterns []string, path string) (string, bool) {
	for _, pattern := range patterns {
		if path == pattern || strings.HasPrefix(path, pattern+"/") {
			return pattern, true
		}
	}
	return "", false
}
