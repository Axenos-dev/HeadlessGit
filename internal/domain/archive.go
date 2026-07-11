package domain

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const archivePrefixMaxLength = 255

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
	Prefix     string
}

func NormalizeArchivePrefix(prefix string) (string, bool) {
	if prefix == "" {
		return "", true
	}
	if len(prefix) > archivePrefixMaxLength || !utf8.ValidString(prefix) {
		return "", false
	}
	if strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, `\:`) {
		return "", false
	}
	for _, r := range prefix {
		if unicode.IsControl(r) {
			return "", false
		}
	}

	prefix = strings.TrimSuffix(prefix, "/")
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
	}
	return prefix + "/", true
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
