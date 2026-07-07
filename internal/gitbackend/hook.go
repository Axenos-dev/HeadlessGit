package gitbackend

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

// hook-mode process entrypoint, shared by the server binary
func HookMain(args []string) int {
	if len(args) != 1 || args[0] != "pre-receive" {
		fmt.Fprintf(os.Stderr, "unknown hook invocation %v\n", args)
		return 1
	}

	policies, err := domain.DecodePathPolicies(os.Getenv(domain.EnvPathPolicies))
	if err != nil {
		fmt.Fprintln(os.Stderr, "policy check failed:", err)
		return 1
	}

	// generous ceiling: history imports can push tens of thousands of commits
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := PreReceive(ctx, ".", os.Stdin, policies); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// implements the pre-receive hook,
// it reads the "old new ref" lines git feeds on stdin,
// walks the commits the push introduces,
// and returns an error describing the first path policy violation
//
// it runs inside the hook process, before any ref has moved
func PreReceive(ctx context.Context, dir string, in io.Reader, policies []domain.PathPolicy) error {
	if len(policies) == 0 {
		return nil
	}

	patterns := make([]string, len(policies))
	reasons := make(map[string]string, len(policies))
	for i, p := range policies {
		patterns[i] = p.Pattern
		if p.Reason != "" {
			reasons[p.Pattern] = p.Reason
		}
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git binary not found: %w", err)
	}

	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return fmt.Errorf("malformed pre-receive input: %q", line)
		}
		newSHA, ref := fields[1], fields[2]

		// deleting a ref adds no content
		if newSHA == zeroSHA {
			continue
		}

		// exactly the commits this push introduces: everything reachable
		// from the new tip minus everything any existing ref already has
		revs, err := hookGit(ctx, gitPath, dir, nil, "rev-list", newSHA, "--not", "--all")
		if err != nil {
			return fmt.Errorf("rev-list %s: %w", ref, err)
		}

		for commit := range strings.SplitSeq(revs, "\n") {
			if commit == "" {
				continue
			}

			out, err := hookGit(ctx, gitPath, dir, nil,
				"diff-tree", "-r", "-c", "--root", "--name-only", "--no-commit-id", "--diff-filter=d", "-z", commit)
			if err != nil {
				return fmt.Errorf("diff-tree %s: %w", commit, err)
			}

			for path := range strings.SplitSeq(out, "\x00") {
				if path == "" {
					continue
				}
				if pattern, blocked := domain.PathBlocked(patterns, path); blocked {
					if reason := reasons[pattern]; reason != "" {
						return fmt.Errorf("push rejected: %q is blocked by policy (%s)", path, reason)
					}
					return fmt.Errorf("push rejected: %q is blocked by policy %q", path, pattern)
				}
			}
		}
	}
	return sc.Err()
}

func hookGit(ctx context.Context, gitPath, dir string, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, gitPath, append([]string{"-C", dir}, args...)...)
	cmd.Stdin = stdin
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
