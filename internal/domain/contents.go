package domain

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
	Name string
	Path string // full path from the repo root
	Type TreeEntryType
	Mode string
	SHA  string
	Size int64 // object size in bytes, -1 for non-blobs (dirs, submodules)
}

type RepositoryContents struct {
	Ref       string
	CommitSHA string
	Path      string
	Entries   []TreeEntry
	Truncated bool
}
