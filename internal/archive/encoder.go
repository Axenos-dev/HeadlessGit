package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"strings"
)

type Encoder interface {
	Write(hdr *tar.Header, body io.Reader) error
	Close() error
}

type zipEncoder struct {
	zw *zip.Writer
}

func NewZipEncoder(out io.Writer) Encoder {
	return &zipEncoder{zw: zip.NewWriter(out)}
}

func (e *zipEncoder) Write(hdr *tar.Header, body io.Reader) error {
	fi := hdr.FileInfo()
	fh, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}

	fh.Name = hdr.Name
	if fi.IsDir() {
		fh.Name = strings.TrimSuffix(fh.Name, "/") + "/"
		fh.Method = zip.Store
	} else {
		fh.Method = zip.Deflate
	}

	w, err := e.zw.CreateHeader(fh)
	if err != nil {
		return err
	}

	switch hdr.Typeflag {
	case tar.TypeReg:
		_, err = io.Copy(w, body)
	case tar.TypeSymlink:
		// zip stores the link target as the entry body
		_, err = io.Copy(w, strings.NewReader(hdr.Linkname))
	}
	return err
}

func (e *zipEncoder) Close() error { return e.zw.Close() }

type tarGzEncoder struct {
	gz *gzip.Writer
	tw *tar.Writer
}

func NewTarGzEncoder(out io.Writer) Encoder {
	gz := gzip.NewWriter(out)
	return &tarGzEncoder{gz: gz, tw: tar.NewWriter(gz)}
}

func (e *tarGzEncoder) Write(hdr *tar.Header, body io.Reader) error {
	if err := e.tw.WriteHeader(hdr); err != nil {
		return err
	}
	if hdr.Typeflag == tar.TypeReg {
		if _, err := io.Copy(e.tw, body); err != nil {
			return err
		}
	}
	return nil
}

func (e *tarGzEncoder) Close() error {
	if err := e.tw.Close(); err != nil {
		return err
	}
	return e.gz.Close()
}
