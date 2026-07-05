package domain

import "fmt"

type ArchiveFormat string

const (
	ArchiveFormatZip   ArchiveFormat = "zip"
	ArchiveFormatTarGz ArchiveFormat = "tar.gz"
)

func ParseArchiveFormat(s string) (ArchiveFormat, bool) {
	switch s {
	case "", "zip":
		return ArchiveFormatZip, true
	case "tar.gz", "tgz":
		return ArchiveFormatTarGz, true
	}
	return "", false
}

func (f ArchiveFormat) Extension() string {
	if f == ArchiveFormatTarGz {
		return "tar.gz"
	}
	return "zip"
}

type ArchiveRequest struct {
	Repository Repository
	CommitSHA  string
	Format     ArchiveFormat
	IncludeLFS bool
}

// Filename is the suggested artifact name: <repo>-<shortsha>.<ext>
func (r ArchiveRequest) Filename() string {
	return fmt.Sprintf("%s-%s.%s", r.Repository.RepositoryName, ShortSHA(r.CommitSHA), r.Format.Extension())
}

func ShortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
