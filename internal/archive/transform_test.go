package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// builds a tar the way git archive would emit it: pax global header first,
// then dirs, files (including an LFS pointer), and a symlink
func buildTestTar(t *testing.T, pointer string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	write := func(hdr *tar.Header, body string) {
		t.Helper()
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}

	write(&tar.Header{
		Typeflag:   tar.TypeXGlobalHeader,
		Name:       "pax_global_header",
		PAXRecords: map[string]string{"comment": strings.Repeat("a", 40)},
		Format:     tar.FormatPAX,
	}, "")
	write(&tar.Header{Typeflag: tar.TypeDir, Name: "src/", Mode: 0o755}, "")
	write(&tar.Header{Typeflag: tar.TypeReg, Name: "README.md", Mode: 0o644, Size: 6}, "hello\n")
	write(&tar.Header{Typeflag: tar.TypeReg, Name: "src/big.bin", Mode: 0o644, Size: int64(len(pointer))}, pointer)
	write(&tar.Header{Typeflag: tar.TypeSymlink, Name: "link", Linkname: "README.md", Mode: 0o777}, "")

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readZip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = string(body)
	}
	return got
}

func smudgeFrom(objects map[string]string) SmudgeFunc {
	return func(oid string) (io.ReadCloser, int64, error) {
		content, ok := objects[oid]
		if !ok {
			return nil, 0, errors.New("object not found")
		}
		return io.NopCloser(strings.NewReader(content)), int64(len(content)), nil
	}
}

func TestTransformZipSmudgesLFS(t *testing.T) {
	oid := strings.Repeat("ab", 32)
	content := "REAL LFS CONTENT, MUCH BIGGER THAN A POINTER"
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, len(content))

	var out bytes.Buffer
	src := bytes.NewReader(buildTestTar(t, pointer))
	if err := Transform(src, "repo-abc/", smudgeFrom(map[string]string{oid: content}), NewZipEncoder(&out)); err != nil {
		t.Fatal(err)
	}

	got := readZip(t, out.Bytes())
	if _, ok := got["repo-abc/src/"]; !ok {
		t.Errorf("missing dir entry, got %d entries", len(got))
	}
	if got["repo-abc/README.md"] != "hello\n" {
		t.Errorf("README.md = %q", got["repo-abc/README.md"])
	}
	if got["repo-abc/src/big.bin"] != content {
		t.Errorf("big.bin not smudged: %q", got["repo-abc/src/big.bin"])
	}
	if got["repo-abc/link"] != "README.md" {
		t.Errorf("symlink target = %q", got["repo-abc/link"])
	}
	if _, ok := got["repo-abc/pax_global_header"]; ok {
		t.Error("pax global header leaked into the archive")
	}
}

func TestTransformKeepsPointerWhenObjectMissing(t *testing.T) {
	oid := strings.Repeat("ab", 32)
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize 44\n", oid)

	var out bytes.Buffer
	src := bytes.NewReader(buildTestTar(t, pointer))
	if err := Transform(src, "repo-abc/", smudgeFrom(nil), NewZipEncoder(&out)); err != nil {
		t.Fatal(err)
	}

	if got := readZip(t, out.Bytes())["repo-abc/src/big.bin"]; got != pointer {
		t.Errorf("missing object should keep pointer bytes, got %q", got)
	}
}

func TestTransformWithoutSmudge(t *testing.T) {
	oid := strings.Repeat("ab", 32)
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize 44\n", oid)

	var out bytes.Buffer
	src := bytes.NewReader(buildTestTar(t, pointer))
	if err := Transform(src, "repo-abc/", nil, NewZipEncoder(&out)); err != nil {
		t.Fatal(err)
	}

	if got := readZip(t, out.Bytes())["repo-abc/src/big.bin"]; got != pointer {
		t.Errorf("nil smudge should keep pointer bytes, got %q", got)
	}
}

func TestTransformWithoutPrefix(t *testing.T) {
	var out bytes.Buffer
	if err := Transform(bytes.NewReader(buildTestTar(t, "not a pointer")), "", nil, NewZipEncoder(&out)); err != nil {
		t.Fatal(err)
	}

	got := readZip(t, out.Bytes())
	if got["README.md"] != "hello\n" {
		t.Errorf("README.md = %q", got["README.md"])
	}
	if _, ok := got["src/"]; !ok {
		t.Errorf("missing root-level directory entry")
	}
}

func TestTransformTarGz(t *testing.T) {
	oid := strings.Repeat("ab", 32)
	content := "REAL LFS CONTENT"
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, len(content))

	var out bytes.Buffer
	src := bytes.NewReader(buildTestTar(t, pointer))
	if err := Transform(src, "repo-abc/", smudgeFrom(map[string]string{oid: content}), NewTarGzEncoder(&out)); err != nil {
		t.Fatal(err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[hdr.Name] = string(body)
	}

	if got["repo-abc/src/big.bin"] != content {
		t.Errorf("big.bin not smudged: %q", got["repo-abc/src/big.bin"])
	}
	if got["repo-abc/README.md"] != "hello\n" {
		t.Errorf("README.md = %q", got["repo-abc/README.md"])
	}
}
