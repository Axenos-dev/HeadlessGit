package gitbackend

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

type indexEntry struct {
	Mode string
	SHA  string
	Path string
}

// creates a commit on a branch from already-uploaded blobs
func (l *Local) ApplyCommit(ctx context.Context, storagePath string, spec CommitSpec, ops []CommitOp, clean CleanFunc, checkWrite CheckWriteFunc) (RefChange, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return RefChange{}, err
	}

	spec, err = l.validateCommitSpec(ctx, dir, spec)
	if err != nil {
		return RefChange{}, err
	}
	ops, err = validateCommitOps(ops)
	if err != nil {
		return RefChange{}, err
	}

	// resolve the current branch head
	ref := "refs/heads/" + spec.Branch
	oldSHA, err := l.revParse(ctx, dir, ref)
	unborn := err != nil
	switch {
	case unborn && spec.ExpectedOld != zeroSHA:
		return RefChange{}, fmt.Errorf("%w: branch %s", ErrRevNotFound, spec.Branch)
	case !unborn && spec.ExpectedOld == zeroSHA:
		return RefChange{}, fmt.Errorf("%w: branch %s already exists", ErrHeadMismatch, spec.Branch)
	case !unborn && spec.ExpectedOld != "" && spec.ExpectedOld != oldSHA:
		return RefChange{}, fmt.Errorf("%w: expected %s, head is %s", ErrHeadMismatch, spec.ExpectedOld, oldSHA)
	}
	if unborn {
		oldSHA = zeroSHA
	}

	ops, err = l.materializeLFSPointers(ctx, dir, ops)
	if err != nil {
		return RefChange{}, err
	}

	sizes, err := l.verifyPutInputs(ctx, dir, ops)
	if err != nil {
		return RefChange{}, err
	}

	// private index file: commits never touch the repo's real index (bare
	// repos have none) and concurrent commits cannot see each other
	idx, err := os.CreateTemp(dir, "headlessgit-index-*")
	if err != nil {
		return RefChange{}, fmt.Errorf("create temp index: %w", err)
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := []string{"GIT_INDEX_FILE=" + idx.Name()}

	if unborn {
		if _, err := l.runGit(ctx, dir, env, nil, "read-tree", "--empty"); err != nil {
			return RefChange{}, err
		}
	} else {
		if _, err := l.runGit(ctx, dir, env, nil, "read-tree", oldSHA); err != nil {
			return RefChange{}, err
		}
	}

	// Stage in request order. A move therefore changes the paths seen by every
	// operation that follows it in the same request.
	puts, err := l.stageCommitOps(ctx, dir, env, ops, checkWrite)
	if err != nil {
		return RefChange{}, err
	}

	// All .gitattributes changes are now in the index. Clean only explicit puts;
	// moved entries keep their existing object ids, just like git mv.
	puts, err = l.cleanLFSTracked(ctx, dir, env, puts, sizes, clean)
	if err != nil {
		return RefChange{}, err
	}
	if err := l.updateIndex(ctx, dir, env, puts); err != nil {
		return RefChange{}, err
	}

	treeSHA, err := l.runGit(ctx, dir, env, nil, "write-tree")
	if err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrInvalidOps, err)
	}
	if !unborn {
		oldTree, err := l.revParse(ctx, dir, oldSHA+"^{tree}")
		if err != nil {
			return RefChange{}, err
		}
		if treeSHA == oldTree {
			return RefChange{}, ErrNothingToCommit
		}
	}

	commitEnv := []string{
		"GIT_AUTHOR_NAME=" + spec.Author.Name,
		"GIT_AUTHOR_EMAIL=" + spec.Author.Email,
		"GIT_COMMITTER_NAME=" + spec.Committer.Name,
		"GIT_COMMITTER_EMAIL=" + spec.Committer.Email,
	}
	args := []string{"commit-tree", treeSHA}
	if !unborn {
		args = append(args, "-p", oldSHA)
	}
	args = append(args, "-m", spec.Message)
	newSHA, err := l.runGit(ctx, dir, commitEnv, nil, args...)
	if err != nil {
		return RefChange{}, err
	}

	// update-ref only moves the branch if it still points at oldSHA
	if _, err := l.runGit(ctx, dir, nil, nil, "update-ref", ref, newSHA, oldSHA); err != nil {
		return RefChange{}, fmt.Errorf("%w: %s", ErrHeadMismatch, err)
	}

	return RefChange{Ref: ref, OldSHA: oldSHA, NewSHA: newSHA}, nil
}

// validateCommitSpec checks the branch name and identities
func (l *Local) validateCommitSpec(ctx context.Context, dir string, spec CommitSpec) (CommitSpec, error) {
	if spec.Branch == "" || strings.HasPrefix(spec.Branch, "-") {
		return spec, fmt.Errorf("%w: %q", ErrInvalidBranch, spec.Branch)
	}
	for _, r := range spec.Branch {
		if r < 0x20 || r == 0x7f {
			return spec, fmt.Errorf("%w: control character", ErrInvalidBranch)
		}
	}
	// git itself is the authority on ref name rules
	if _, err := l.runGit(ctx, dir, nil, nil, "check-ref-format", "--branch", spec.Branch); err != nil {
		return spec, fmt.Errorf("%w: %q", ErrInvalidBranch, spec.Branch)
	}

	if spec.ExpectedOld != "" && !isHexSHA(spec.ExpectedOld) {
		return spec, fmt.Errorf("%w: expected old %q", ErrInvalidRev, spec.ExpectedOld)
	}
	if spec.Author.Name == "" || spec.Author.Email == "" {
		return spec, fmt.Errorf("%w: author name and email are required", ErrInvalidOps)
	}
	if spec.Committer.Name == "" {
		spec.Committer = spec.Author
	}
	if spec.Message == "" {
		return spec, fmt.Errorf("%w: message is required", ErrInvalidOps)
	}
	return spec, nil
}

// normalizes paths and enforces the op rules
func validateCommitOps(ops []CommitOp) ([]CommitOp, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: no operations", ErrInvalidOps)
	}
	if len(ops) > maxCommitOps {
		return nil, fmt.Errorf("%w: more than %d operations", ErrInvalidOps, maxCommitOps)
	}

	out := make([]CommitOp, len(ops))
	seen := make(map[string]bool, len(ops))
	for i, op := range ops {
		p, err := normalizeTreePath(op.Path)
		if err != nil || p == "" {
			return nil, fmt.Errorf("%w: path %q", ErrInvalidOps, op.Path)
		}
		// the batch-check and check-attr line protocols cannot carry these
		for _, r := range p {
			if r < 0x20 || r == 0x7f {
				return nil, fmt.Errorf("%w: control character in path", ErrInvalidOps)
			}
		}
		op.Path = p
		if op.MoveFrom != "" {
			from, err := normalizeTreePath(op.MoveFrom)
			if err != nil || from == "" {
				return nil, fmt.Errorf("%w: source path %q", ErrInvalidOps, op.MoveFrom)
			}
			for _, r := range from {
				if r < 0x20 || r == 0x7f {
					return nil, fmt.Errorf("%w: control character in source path", ErrInvalidOps)
				}
			}
			if op.Delete || op.BlobSHA != "" || op.Lfs != nil || op.Mode != "" {
				return nil, fmt.Errorf("%w: move %q takes no object or mode", ErrInvalidOps, from)
			}
			if from == p || strings.HasPrefix(p, from+"/") {
				return nil, fmt.Errorf("%w: cannot move %q to %q", ErrInvalidOps, from, p)
			}
			op.MoveFrom = from
			out[i] = op
			continue
		}

		for other := range seen {
			if p == other {
				return nil, fmt.Errorf("%w: duplicate path %q", ErrInvalidOps, p)
			}
			if strings.HasPrefix(p, other+"/") || strings.HasPrefix(other, p+"/") {
				return nil, fmt.Errorf("%w: overlapping paths %q and %q", ErrInvalidOps, other, p)
			}
		}
		seen[p] = true

		if op.Delete {
			if op.BlobSHA != "" || op.Lfs != nil {
				return nil, fmt.Errorf("%w: delete %q takes no object", ErrInvalidOps, p)
			}
		} else {
			hasBlob := op.BlobSHA != ""
			hasLFS := op.Lfs != nil

			if hasBlob == hasLFS {
				return nil, fmt.Errorf("%w: put %q requires exactly one of blob sha or lfs object", ErrInvalidOps, p)
			}
			if hasBlob && !isHexSHA(op.BlobSHA) {
				return nil, fmt.Errorf("%w: blob sha %q", ErrInvalidOps, op.BlobSHA)
			}
			if hasLFS && (!isLFSOID(op.Lfs.OID) || op.Lfs.Size < 0) {
				return nil, fmt.Errorf("%w: invalid lfs object for %q", ErrInvalidOps, p)
			}
			if hasLFS && isAttributesPath(p) {
				return nil, fmt.Errorf("%w: attributes file %q cannot be an lfs object", ErrInvalidOps, p)
			}
			switch op.Mode {
			case "":
				op.Mode = "100644"
			case "100644", "100755":
			default:
				return nil, fmt.Errorf("%w: mode %q", ErrInvalidOps, op.Mode)
			}
		}
		out[i] = op
	}
	return out, nil
}

// transforms lfs objects to its pointers
func (l *Local) materializeLFSPointers(ctx context.Context, dir string, ops []CommitOp) ([]CommitOp, error) {
	for i := range ops {
		if ops[i].Lfs == nil {
			continue
		}

		pointer := fmt.Sprintf(
			"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
			ops[i].Lfs.OID,
			ops[i].Lfs.Size,
		)
		// generate sha for handcrafted pointer
		sha, err := l.runGit(ctx, dir, nil, strings.NewReader(pointer), "hash-object", "-w", "--stdin")
		if err != nil {
			return nil, fmt.Errorf("write lfs pointer for %q: %w", ops[i].Path, err)
		}

		if !isHexSHA(sha) {
			return nil, fmt.Errorf("write lfs pointer for %q returned invalid sha %q", ops[i].Path, sha)
		}

		ops[i].BlobSHA = sha
	}
	return ops, nil
}

// runs one cat-file --batch-check over every put blob sha (returning their sizes)
func (l *Local) verifyPutInputs(ctx context.Context, dir string, ops []CommitOp) (map[string]int64, error) {
	var in strings.Builder
	var puts []CommitOp
	for _, op := range ops {
		if op.Delete || op.MoveFrom != "" {
			continue
		}
		in.WriteString(op.BlobSHA + "\n")
		puts = append(puts, op)
	}
	if len(puts) == 0 {
		return map[string]int64{}, nil
	}

	out, err := l.runGit(ctx, dir, nil, strings.NewReader(in.String()), "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(out, "\n")
	if len(lines) != len(puts) {
		return nil, fmt.Errorf("unexpected batch-check output: %d lines for %d queries", len(lines), len(puts))
	}

	sizes := make(map[string]int64)
	for i, line := range lines {
		op := puts[i]
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 2 && fields[len(fields)-1] == "missing":
			return nil, fmt.Errorf("%w: %s", ErrUnknownBlob, op.BlobSHA)
		case len(fields) == 3 && fields[1] == "blob":
			size, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed batch-check size %q: %w", fields[2], err)
			}
			sizes[op.BlobSHA] = size
		case len(fields) == 3:
			return nil, fmt.Errorf("%w: %s is a %s", ErrUnknownBlob, op.BlobSHA, fields[1])
		default:
			return nil, fmt.Errorf("malformed batch-check line: %q", line)
		}
	}
	return sizes, nil
}

// apply ops sequentially against temporary index, and return the ops that "survived"
func (l *Local) stageCommitOps(ctx context.Context, dir string, env []string, ops []CommitOp, checkWrite CheckWriteFunc) ([]CommitOp, error) {
	pendingPuts := make(map[string]CommitOp)

	for _, op := range ops {
		switch {
		case op.MoveFrom != "":
			if err := l.moveIndexPath(ctx, dir, env, op.MoveFrom, op.Path, pendingPuts, checkWrite); err != nil {
				return nil, err
			}

		case op.Delete:
			entries, err := l.listIndexEntries(ctx, dir, env, op.Path)
			if err != nil {
				return nil, err
			}
			if len(entries) == 0 {
				return nil, fmt.Errorf("%w: %q", ErrPathNotFound, op.Path)
			}

			deletes := make([]CommitOp, len(entries))
			for i, entry := range entries {
				deletes[i] = CommitOp{Delete: true, Path: entry.Path}
			}
			if err := l.updateIndex(ctx, dir, env, deletes); err != nil {
				return nil, err
			}
			// no puts can happen under this path, after delete
			removePendingPuts(pendingPuts, op.Path)

		default:
			if checkWrite != nil {
				if err := checkWrite(op.Path); err != nil {
					return nil, err
				}
			}
			if err := l.updateIndex(ctx, dir, env, []CommitOp{op}); err != nil {
				return nil, err
			}
			removePendingPuts(pendingPuts, op.Path)
			pendingPuts[op.Path] = op
		}
	}

	paths := make([]string, 0, len(pendingPuts))
	for path := range pendingPuts {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	puts := make([]CommitOp, 0, len(paths))
	for _, path := range paths {
		puts = append(puts, pendingPuts[path])
	}
	return puts, nil
}

func (l *Local) moveIndexPath(ctx context.Context, dir string, env []string, from, destination string, pendingPuts map[string]CommitOp, checkWrite CheckWriteFunc) error {
	entries, err := l.listIndexEntries(ctx, dir, env, from)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("%w: %q", ErrPathNotFound, from)
	}

	occupied, err := l.listIndexEntries(ctx, dir, env, destination)
	if err != nil {
		return err
	}
	parentOccupied, err := l.indexParentOccupied(ctx, dir, env, destination)
	if err != nil {
		return err
	}
	if len(occupied) != 0 || parentOccupied {
		return fmt.Errorf("%w: %q", ErrPathExists, destination)
	}

	changes := make([]CommitOp, 0, len(entries)*2)
	for _, entry := range entries {
		changes = append(changes, CommitOp{Delete: true, Path: entry.Path})
	}
	for _, entry := range entries {
		target := destination + strings.TrimPrefix(entry.Path, from)
		if checkWrite != nil {
			if err := checkWrite(target); err != nil {
				return err
			}
		}
		changes = append(changes, CommitOp{Path: target, BlobSHA: entry.SHA, Mode: entry.Mode})
	}

	if err := l.updateIndex(ctx, dir, env, changes); err != nil {
		return err
	}
	movePendingPuts(pendingPuts, from, destination)
	return nil
}

// git ls-files at treePath
func (l *Local) listIndexEntries(ctx context.Context, dir string, env []string, treePath string) ([]indexEntry, error) {
	out, err := l.runGitBytes(ctx, dir, env, nil,
		"ls-files", "--stage", "-z", "--full-name", "--", ":(top,literal)"+treePath,
	)
	if err != nil {
		return nil, err
	}

	var entries []indexEntry
	for record := range bytes.SplitSeq(out, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, name, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			return nil, fmt.Errorf("malformed ls-files record: %q", record)
		}
		fields := strings.Fields(string(header))
		if len(fields) != 3 || !isHexSHA(fields[1]) || fields[2] != "0" {
			return nil, fmt.Errorf("malformed ls-files header: %q", header)
		}
		entries = append(entries, indexEntry{Mode: fields[0], SHA: fields[1], Path: string(name)})
	}
	return entries, nil
}

// checks wheather the destination is free AND non of the parent path is a file.
// e.g. dest="a/b", but "a" is a file and not a dir
func (l *Local) indexParentOccupied(ctx context.Context, dir string, env []string, destination string) (bool, error) {
	var parents []string
	for parent := path.Dir(destination); parent != "."; parent = path.Dir(parent) {
		parents = append(parents, parent)
	}
	if len(parents) == 0 {
		return false, nil
	}

	var in strings.Builder
	for _, parent := range parents {
		in.WriteString(":" + parent + "\n")
	}
	out, err := l.runGit(ctx, dir, env, strings.NewReader(in.String()), "cat-file", "--batch-check")
	if err != nil {
		return false, err
	}

	lines := strings.Split(out, "\n")
	if len(lines) != len(parents) {
		return false, fmt.Errorf("unexpected index batch-check output: %d lines for %d paths", len(lines), len(parents))
	}

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return false, fmt.Errorf("malformed index batch-check line: %q", line)
		}
		// if some of the parent paths are NOT missing => it exists as a file, which is bad
		if fields[len(fields)-1] != "missing" {
			return true, nil
		}
	}
	return false, nil
}

func removePendingPuts(pending map[string]CommitOp, treePath string) {
	for path := range pending {
		if path == treePath || strings.HasPrefix(path, treePath+"/") {
			delete(pending, path)
		}
	}
}

func movePendingPuts(pending map[string]CommitOp, from, destination string) {
	moved := make(map[string]CommitOp)
	for path, op := range pending {
		if path != from && !strings.HasPrefix(path, from+"/") {
			continue
		}
		delete(pending, path)
		op.Path = destination + strings.TrimPrefix(path, from)
		moved[op.Path] = op
	}
	maps.Copy(pending, moved)
}

// asks git which put paths are lfs-tracked,
// and swaps their blobs for the pointer blobs produced by the clean filter
func (l *Local) cleanLFSTracked(ctx context.Context, dir string, env []string, ops []CommitOp, sizes map[string]int64, clean CleanFunc) ([]CommitOp, error) {
	var paths []string
	byPath := make(map[string]int)
	pendingLFS := make(map[string]struct{})

	for i, op := range ops {
		if !op.Delete {
			paths = append(paths, op.Path)
			byPath[op.Path] = i
			if op.Lfs != nil {
				pendingLFS[op.Path] = struct{}{}
			}
		}
	}
	if len(paths) == 0 {
		return ops, nil
	}

	args := append([]string{"check-attr", "-z", "--cached", "filter", "--"}, paths...)
	out, err := l.runGit(ctx, dir, env, nil, args...)
	if err != nil {
		return nil, err
	}

	// -z output is NUL-separated (path, attr, value) triples
	fields := strings.Split(out, "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		path, value := fields[i], fields[i+2]
		idx, ok := byPath[path]
		if !ok {
			continue
		}

		if ops[idx].Lfs != nil {
			delete(pendingLFS, path)
			// do not allow commiting lfs objects, if they are not tracked as lfs
			if value != "lfs" {
				return nil, fmt.Errorf("%w: %q", ErrLFSNotTracked, path)
			}
			continue
		}

		if value != "lfs" {
			continue
		}
		if clean == nil {
			return nil, fmt.Errorf("%w: %q", ErrLFSRequired, path)
		}

		pointerSHA, err := clean(path, ops[idx].BlobSHA, sizes[ops[idx].BlobSHA])
		if err != nil {
			return nil, fmt.Errorf("lfs clean %q: %w", path, err)
		}
		if !isHexSHA(pointerSHA) {
			return nil, fmt.Errorf("lfs clean %q returned invalid sha %q", path, pointerSHA)
		}
		ops[idx].BlobSHA = pointerSHA
	}

	if len(pendingLFS) != 0 {
		return nil, fmt.Errorf("check-attr omitted explicit lfs paths")
	}

	return ops, nil
}

// applies puts and deletes in one subprocess
func (l *Local) updateIndex(ctx context.Context, dir string, env []string, ops []CommitOp) error {
	if len(ops) == 0 {
		return nil
	}
	var in strings.Builder
	for _, op := range ops {
		if op.Delete {
			in.WriteString("0 " + zeroSHA + "\t" + op.Path + "\x00")
		} else {
			in.WriteString(op.Mode + " " + op.BlobSHA + "\t" + op.Path + "\x00")
		}
	}
	if _, err := l.runGit(ctx, dir, env, strings.NewReader(in.String()), "update-index", "-z", "--index-info"); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidOps, err)
	}
	return nil
}
