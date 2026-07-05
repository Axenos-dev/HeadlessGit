package archive

import (
	"archive/tar"
	"bytes"
	"io"
)

type SmudgeFunc func(oid string) (io.ReadCloser, int64, error)

func Transform(src io.Reader, prefix string, smudge SmudgeFunc, enc Encoder) error {
	tr := tar.NewReader(src)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// drop the pax global header (the commit sha comment git emits)
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		hdr.Name = prefix + hdr.Name

		if err := writeEntry(enc, hdr, tr, smudge); err != nil {
			return err
		}
	}
	return enc.Close()
}

func writeEntry(enc Encoder, hdr *tar.Header, body io.Reader, smudge SmudgeFunc) error {
	// only small regular files can be LFS pointers, everything else passes through
	if smudge == nil || hdr.Typeflag != tar.TypeReg || hdr.Size > lfsPointerMaxSize {
		return enc.Write(hdr, body)
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	ptr, ok := parseLFSPointer(data)
	if !ok {
		return enc.Write(hdr, bytes.NewReader(data))
	}

	rc, size, err := smudge(ptr.oid)
	if err != nil {
		// missing or foreign object: keep the pointer bytes, never fail the archive
		return enc.Write(hdr, bytes.NewReader(data))
	}
	defer rc.Close()

	hdr.Size = size
	return enc.Write(hdr, rc)
}
