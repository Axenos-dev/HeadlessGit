package domain

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	EnvPathPolicies = "HEADLESSGIT_POLICIES" // json array of {pattern, reason}
	EnvHookBin      = "HEADLESSGIT_BIN"      // absolute path of the server binary
)

type envPathPolicy struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason,omitempty"`
}

// serializes policies for the hook env var
func EncodePathPolicies(policies []PathPolicy) (string, error) {
	wire := make([]envPathPolicy, len(policies))
	for i, p := range policies {
		wire[i] = envPathPolicy{
			Pattern: p.Pattern,
			Reason:  p.Reason,
		}
	}
	out, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func HookEnv(binPath string, policies []PathPolicy) ([]string, error) {
	env := []string{EnvHookBin + "=" + binPath}
	if len(policies) > 0 {
		encoded, err := EncodePathPolicies(policies)
		if err != nil {
			return nil, err
		}
		env = append(env, EnvPathPolicies+"="+encoded)
	}
	return env, nil
}

// parses the hook env var
func DecodePathPolicies(s string) ([]PathPolicy, error) {
	if s == "" {
		return nil, nil
	}
	var wire []envPathPolicy
	if err := json.Unmarshal([]byte(s), &wire); err != nil {
		return nil, err
	}
	out := make([]PathPolicy, len(wire))
	for i, p := range wire {
		out[i] = PathPolicy{Pattern: p.Pattern, Reason: p.Reason, Kind: PathPolicyBlock}
	}
	return out, nil
}

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
