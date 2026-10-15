package gitbackend

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func (l *Local) ListBranches(ctx context.Context, storagePath string) (BranchListing, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return BranchListing{}, err
	}

	head, err := l.headRef(ctx, dir)
	if err != nil {
		return BranchListing{}, err
	}

	// one ref past the cap tells a full listing from a truncated one
	out, err := l.runGit(ctx, dir, nil, nil,
		"for-each-ref", "--count="+strconv.Itoa(maxBranches+1), "--format=%(objectname) %(refname)", "refs/heads/",
	)
	if err != nil {
		return BranchListing{}, err
	}

	branches, truncated, err := parseBranches(out)
	if err != nil {
		return BranchListing{}, err
	}

	return BranchListing{
		Default:   strings.TrimPrefix(head, "refs/heads/"),
		Branches:  branches,
		Truncated: truncated,
	}, nil
}

// points a new branch at an existing commit
func (l *Local) CreateBranch(ctx context.Context, storagePath, name, sha string) (RefChange, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return RefChange{}, err
	}

	if err := l.validateBranchName(ctx, dir, name); err != nil {
		return RefChange{}, err
	}
	if !isHexSHA(sha) {
		return RefChange{}, fmt.Errorf("%w: %q", ErrInvalidRev, sha)
	}

	// ^{commit} requires the object to exist and peels an annotated tag to its commit
	commitSHA, err := l.revParse(ctx, dir, sha+"^{commit}")
	if err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrRevNotFound, sha)
	}

	// the all-zero old value makes update-ref refuse an existing ref,
	// and a name that collides with one (feature vs feature/x)
	ref := "refs/heads/" + name
	if _, err := l.runGit(ctx, dir, nil, nil, "update-ref", ref, commitSHA, zeroSHA); err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrBranchExists, err)
	}

	return RefChange{Ref: ref, OldSHA: zeroSHA, NewSHA: commitSHA}, nil
}

// deletes a branch, pinned to expectedOld when it is set.
// the branch HEAD points at is refused, so clones never land on a missing ref
func (l *Local) DeleteBranch(ctx context.Context, storagePath, name, expectedOld string) (RefChange, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return RefChange{}, err
	}

	if err := l.validateBranchName(ctx, dir, name); err != nil {
		return RefChange{}, err
	}
	if expectedOld != "" && !isHexSHA(expectedOld) {
		return RefChange{}, fmt.Errorf("%w: expected old %q", ErrInvalidRev, expectedOld)
	}

	ref := "refs/heads/" + name
	oldSHA, err := l.revParse(ctx, dir, ref)
	if err != nil {
		return RefChange{}, fmt.Errorf("%w: branch %s", ErrRevNotFound, name)
	}

	head, err := l.headRef(ctx, dir)
	if err != nil {
		return RefChange{}, err
	}
	if head == ref {
		return RefChange{}, fmt.Errorf("%w: %s", ErrDefaultBranch, name)
	}

	if expectedOld != "" && expectedOld != oldSHA {
		return RefChange{}, fmt.Errorf("%w: expected %s, head is %s", ErrHeadMismatch, expectedOld, oldSHA)
	}

	// the old value makes the delete lose to a push that moved the branch meanwhile,
	// and --no-deref never follows a symbolic ref into deleting its target
	if _, err := l.runGit(ctx, dir, nil, nil, "update-ref", "--no-deref", "-d", ref, oldSHA); err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrHeadMismatch, err)
	}

	return RefChange{Ref: ref, OldSHA: oldSHA, NewSHA: zeroSHA}, nil
}

// rejects names git would refuse, and names that could read as an option or a full ref
func (l *Local) validateBranchName(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "refs/") {
		return fmt.Errorf("%w: %q", ErrInvalidBranch, name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control character", ErrInvalidBranch)
		}
	}

	// git itself is the authority on ref name rules
	if _, err := l.runGit(ctx, dir, nil, nil, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidBranch, name)
	}
	return nil
}

// the full ref HEAD points at, which may not exist yet in an empty repo
func (l *Local) headRef(ctx context.Context, dir string) (string, error) {
	return l.runGit(ctx, dir, nil, nil, "symbolic-ref", "HEAD")
}

func parseBranches(out string) ([]Branch, bool, error) {
	branches := make([]Branch, 0)
	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue
		}
		if len(branches) == maxBranches {
			return branches, true, nil
		}

		// ref names cannot contain spaces, so the first one ends the sha
		sha, ref, ok := strings.Cut(line, " ")
		if !ok || !isHexSHA(sha) {
			return nil, false, fmt.Errorf("malformed for-each-ref line: %q", line)
		}
		name, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok || name == "" {
			return nil, false, fmt.Errorf("malformed for-each-ref line: %q", line)
		}

		branches = append(branches, Branch{Name: name, CommitSHA: sha})
	}
	return branches, false, nil
}
