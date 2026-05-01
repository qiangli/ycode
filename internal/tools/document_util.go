package tools

import (
	"archive/zip"
	"io"
	"os"
)

// newZIPReader creates a zip.Reader from an open file.
func newZIPReader(f *os.File, size int64) (*zip.Reader, error) {
	return zip.NewReader(f, size)
}

// readAll reads up to maxBytes from a reader.
func readAll(r io.Reader, maxBytes int) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, int64(maxBytes)))
}
