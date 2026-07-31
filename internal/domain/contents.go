package domain

import "time"

type TreeEntryType string

const (
	TreeEntryFile      TreeEntryType = "file"
	TreeEntryDir       TreeEntryType = "dir"
	TreeEntrySymlink   TreeEntryType = "symlink"
	TreeEntrySubmodule TreeEntryType = "submodule"
)

func TreeEntryTypeFromMode(mode string) TreeEntryType {
	switch mode {
	case "040000":
		return TreeEntryDir
	case "120000":
		return TreeEntrySymlink
	case "160000":
		return TreeEntrySubmodule
	default: // 100644, 100755
		return TreeEntryFile
	}
}

type TreeEntry struct {
	Name       string
	Path       string // full path from the repo root
	Type       TreeEntryType
	Mode       string
	SHA        string
	Size       int64 // object size in bytes, -1 for non-blobs (dirs, submodules)
	LastCommit *CommitSummary
}

type CommitSummary struct {
	SHA         string
	Message     string
	CommittedAt time.Time
}

type ContentsOptions struct {
	IncludeLastCommit bool
}

type RepositoryContents struct {
	Ref       string
	CommitSHA string
	Path      string
	Entries   []TreeEntry
	Truncated bool
}

type DiffStatus string

const (
	DiffAdded       DiffStatus = "added"
	DiffModified    DiffStatus = "modified"
	DiffDeleted     DiffStatus = "deleted"
	DiffRenamed     DiffStatus = "renamed"
	DiffCopied      DiffStatus = "copied"
	DiffTypeChanged DiffStatus = "type_changed"
)

type DiffPatchOmittedReason string

const (
	DiffPatchBinary              DiffPatchOmittedReason = "binary"
	DiffPatchTooLarge            DiffPatchOmittedReason = "too_large"
	DiffPatchUnsupportedEncoding DiffPatchOmittedReason = "unsupported_encoding"
)

type DiffFile struct {
	Status             DiffStatus
	OldPath            string
	NewPath            string
	OldBlobSHA         string
	NewBlobSHA         string
	OldMode            string
	NewMode            string
	Additions          int64
	Deletions          int64
	Binary             bool
	Patch              *string
	PatchOmittedReason DiffPatchOmittedReason
}

type RepositoryDiff struct {
	BaseSHA   string
	HeadSHA   string
	Files     []DiffFile
	Truncated bool
}
