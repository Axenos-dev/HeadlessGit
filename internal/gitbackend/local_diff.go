package gitbackend

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (l *Local) Diff(ctx context.Context, storagePath, base, head string) (DiffResult, error) {
	dir, err := l.resolve(storagePath)
	if err != nil {
		return DiffResult{}, err
	}

	emptyTreeSHA := ""
	if base == zeroSHA || head == zeroSHA {
		emptyTreeSHA, err = l.runGit(ctx, dir, nil, strings.NewReader(""), "hash-object", "-t", "tree", "--stdin")
		if err != nil {
			return DiffResult{}, fmt.Errorf("resolve empty tree: %w", err)
		}
		if !isHexSHA(emptyTreeSHA) {
			return DiffResult{}, fmt.Errorf("git returned invalid empty tree sha %q", emptyTreeSHA)
		}
	}

	baseSHA, baseTree, err := l.resolveDiffRevision(ctx, storagePath, base, emptyTreeSHA)
	if err != nil {
		return DiffResult{}, err
	}

	headSHA, headTree, err := l.resolveDiffRevision(ctx, storagePath, head, emptyTreeSHA)
	if err != nil {
		return DiffResult{}, err
	}

	commonArgs := []string{
		"-r",
		"--no-commit-id",
		"--find-renames",
		"--find-copies",
		"--no-ext-diff",
		"--no-textconv",
	}
	rawArgs := append([]string{"diff-tree"}, commonArgs...)
	rawArgs = append(rawArgs, "--raw", "-z", "--abbrev=40", baseTree, headTree, "--")
	out, err := l.runGitBytes(ctx, dir, nil, nil, rawArgs...)
	if err != nil {
		return DiffResult{}, err
	}

	files, truncated, err := parseRawDiff(out)
	if err != nil {
		return DiffResult{}, err
	}
	if len(files) == 0 {
		return DiffResult{BaseSHA: baseSHA, HeadSHA: headSHA, Files: []DiffFile{}}, nil
	}

	statArgs := append([]string{"diff-tree"}, commonArgs...)
	statArgs = append(statArgs, "--numstat", "-z", baseTree, headTree, "--")
	out, err = l.runGitBytes(ctx, dir, nil, nil, statArgs...)
	if err != nil {
		return DiffResult{}, err
	}

	stats, err := parseNumstat(out, len(files))
	if err != nil {
		return DiffResult{}, err
	}

	for i := range files {
		stat, ok := stats[diffFileKey(files[i])]
		if !ok {
			return DiffResult{}, fmt.Errorf("numstat output missing file %q", files[i].NewPath)
		}
		files[i].Additions = stat.additions
		files[i].Deletions = stat.deletions
		files[i].Binary = stat.binary
	}

	if truncated {
		for i := range files {
			if files[i].Binary {
				files[i].PatchOmittedReason = DiffPatchBinary
			} else {
				files[i].PatchOmittedReason = DiffPatchTooLarge
			}
		}
	} else if err := l.addDiffPatches(ctx, dir, baseTree, headTree, files); err != nil {
		return DiffResult{}, err
	}

	return DiffResult{
		BaseSHA:   baseSHA,
		HeadSHA:   headSHA,
		Files:     files,
		Truncated: truncated,
	}, nil
}

func (l *Local) resolveDiffRevision(ctx context.Context, storagePath, rev, emptyTreeSHA string) (string, string, error) {
	if rev == zeroSHA {
		return zeroSHA, emptyTreeSHA, nil
	}

	commitSHA, err := l.ResolveCommit(ctx, storagePath, rev)
	if err != nil {
		return "", "", err
	}
	return commitSHA, commitSHA, nil
}

func (l *Local) addDiffPatches(ctx context.Context, dir, baseTree, headTree string, files []DiffFile) error {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	args := []string{
		"-C",
		dir,
		"diff-tree",
		"-r",
		"--no-commit-id",
		"--patch",
		"--full-index",
		"--unified=3",
		"--no-color",
		"--src-prefix=a/",
		"--dst-prefix=b/",
		"--find-renames",
		"--find-copies",
		"--no-ext-diff",
		"--no-textconv",
		baseTree,
		headTree,
		"--",
	}
	cmd := exec.CommandContext(ctx, l.gitPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git diff-tree stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git diff-tree: %w", err)
	}

	collector := newDiffPatchCollector(files)
	reader := bufio.NewReaderSize(stdout, 64<<10)
	atLineStart := true
	var readErr error
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			if atLineStart && bytes.HasPrefix(chunk, []byte("diff --git ")) {
				collector.startSection()
			}
			collector.add(chunk)
			atLineStart = chunk[len(chunk)-1] == '\n'
		}

		switch err {
		case nil:
			continue
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
		default:
			readErr = err
		}
		break
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git diff-tree: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if readErr != nil {
		return fmt.Errorf("read git diff-tree patch: %w", readErr)
	}
	if err := collector.finish(); err != nil {
		return err
	}
	return nil
}

func parseRawDiff(out []byte) ([]DiffFile, bool, error) {
	var files []DiffFile
	offset := 0
	for offset < len(out) {
		header, ok := nextNULRecord(out, &offset)
		if !ok {
			return nil, false, errors.New("malformed raw diff header")
		}
		if len(header) == 0 {
			continue
		}

		fields := strings.Fields(string(header))
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") {
			return nil, false, fmt.Errorf("malformed raw diff header: %q", header)
		}
		if !isHexSHA(fields[2]) || !isHexSHA(fields[3]) || fields[4] == "" {
			return nil, false, fmt.Errorf("malformed raw diff header: %q", header)
		}

		file := DiffFile{
			OldMode:    strings.TrimPrefix(fields[0], ":"),
			NewMode:    fields[1],
			OldBlobSHA: fields[2],
			NewBlobSHA: fields[3],
		}
		if file.OldMode == "000000" {
			file.OldMode = ""
		}
		if file.NewMode == "000000" {
			file.NewMode = ""
		}
		if file.OldBlobSHA == zeroSHA {
			file.OldBlobSHA = ""
		}
		if file.NewBlobSHA == zeroSHA {
			file.NewBlobSHA = ""
		}

		firstPath, ok := nextNULRecord(out, &offset)
		if !ok || len(firstPath) == 0 {
			return nil, false, errors.New("malformed raw diff path")
		}
		switch fields[4][0] {
		case 'A':
			file.Status = DiffAdded
			file.NewPath = string(firstPath)
		case 'M':
			file.Status = DiffModified
			file.OldPath = string(firstPath)
			file.NewPath = string(firstPath)
		case 'D':
			file.Status = DiffDeleted
			file.OldPath = string(firstPath)
		case 'R', 'C':
			secondPath, ok := nextNULRecord(out, &offset)
			if !ok || len(secondPath) == 0 {
				return nil, false, errors.New("malformed rename or copy path")
			}
			if fields[4][0] == 'R' {
				file.Status = DiffRenamed
			} else {
				file.Status = DiffCopied
			}
			file.OldPath = string(firstPath)
			file.NewPath = string(secondPath)
		case 'T':
			file.Status = DiffTypeChanged
			file.OldPath = string(firstPath)
			file.NewPath = string(firstPath)
		default:
			return nil, false, fmt.Errorf("unsupported diff status %q", fields[4])
		}

		if len(files) == maxDiffEntries {
			return files, true, nil
		}
		files = append(files, file)
	}
	return files, false, nil
}

type diffStat struct {
	additions int64
	deletions int64
	binary    bool
}

func parseNumstat(out []byte, limit int) (map[string]diffStat, error) {
	stats := make(map[string]diffStat, limit)
	offset := 0
	for offset < len(out) && len(stats) < limit {
		record, ok := nextNULRecord(out, &offset)
		if !ok {
			return nil, errors.New("malformed numstat record")
		}
		addRaw, rest, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			return nil, fmt.Errorf("malformed numstat record: %q", record)
		}
		deleteRaw, filePath, ok := bytes.Cut(rest, []byte{'\t'})
		if !ok {
			return nil, fmt.Errorf("malformed numstat record: %q", record)
		}

		key := "p\x00" + string(filePath)
		if len(filePath) == 0 {
			oldPath, ok := nextNULRecord(out, &offset)
			if !ok || len(oldPath) == 0 {
				return nil, errors.New("malformed numstat old path")
			}
			newPath, ok := nextNULRecord(out, &offset)
			if !ok || len(newPath) == 0 {
				return nil, errors.New("malformed numstat new path")
			}
			key = "r\x00" + string(oldPath) + "\x00" + string(newPath)
		}

		stat := diffStat{}
		switch {
		case string(addRaw) == "-" && string(deleteRaw) == "-":
			stat.binary = true
		default:
			additions, err := strconv.ParseInt(string(addRaw), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed addition count %q: %w", addRaw, err)
			}
			deletions, err := strconv.ParseInt(string(deleteRaw), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("malformed deletion count %q: %w", deleteRaw, err)
			}
			stat.additions = additions
			stat.deletions = deletions
		}
		stats[key] = stat
	}
	return stats, nil
}

type diffPatchCollector struct {
	files        []DiffFile
	current      int
	sections     int
	totalBytes   int
	currentPatch []byte
	tooLarge     bool
	err          error
}

func newDiffPatchCollector(files []DiffFile) *diffPatchCollector {
	return &diffPatchCollector{files: files, current: -1}
}

func (c *diffPatchCollector) startSection() {
	c.finalizeSection()
	c.current = c.sections
	c.sections++
	c.currentPatch = nil
	c.tooLarge = false
	if c.current >= len(c.files) && c.err == nil {
		c.err = fmt.Errorf("git patch output has more than %d files", len(c.files))
	}
}

func (c *diffPatchCollector) add(chunk []byte) {
	if c.current < 0 || c.current >= len(c.files) || c.files[c.current].Binary || c.tooLarge {
		return
	}
	if len(c.currentPatch)+len(chunk) > maxFilePatchBytes ||
		c.totalBytes+len(c.currentPatch)+len(chunk) > maxDiffPatchBytes {
		c.currentPatch = nil
		c.tooLarge = true
		return
	}
	c.currentPatch = append(c.currentPatch, chunk...)
}

func (c *diffPatchCollector) finalizeSection() {
	if c.current < 0 || c.current >= len(c.files) {
		return
	}

	file := &c.files[c.current]
	switch {
	case file.Binary:
		file.PatchOmittedReason = DiffPatchBinary
	case c.tooLarge:
		file.PatchOmittedReason = DiffPatchTooLarge
	case !utf8.Valid(c.currentPatch):
		file.PatchOmittedReason = DiffPatchUnsupportedEncoding
	default:
		patch := string(c.currentPatch)
		file.Patch = &patch
		c.totalBytes += len(c.currentPatch)
	}
}

func (c *diffPatchCollector) finish() error {
	c.finalizeSection()
	if c.err != nil {
		return c.err
	}
	if c.sections != len(c.files) {
		return fmt.Errorf("git patch output has %d files, expected %d", c.sections, len(c.files))
	}
	return nil
}

func diffFileKey(file DiffFile) string {
	if file.Status == DiffRenamed || file.Status == DiffCopied {
		return "r\x00" + file.OldPath + "\x00" + file.NewPath
	}
	if file.NewPath != "" {
		return "p\x00" + file.NewPath
	}
	return "p\x00" + file.OldPath
}

func nextNULRecord(out []byte, offset *int) ([]byte, bool) {
	if *offset >= len(out) {
		return nil, false
	}
	end := bytes.IndexByte(out[*offset:], 0)
	if end < 0 {
		return nil, false
	}
	record := out[*offset : *offset+end]
	*offset += end + 1
	return record, true
}
