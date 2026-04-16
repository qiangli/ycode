package api

import (
	"bytes"
	"compress/gzip"
	"io"
)

const (
	// MinCompressSize is the minimum request body size (bytes) to trigger compression.
	// Below this threshold, compression overhead exceeds savings.
	MinCompressSize = 4096
)

// CompressGzip compresses data using gzip.
// Returns the compressed data and "gzip" encoding string, or the original data
// and empty string if compression would not be beneficial.
func CompressGzip(data []byte) ([]byte, string) {
	if len(data) < MinCompressSize {
		return data, ""
	}

	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return data, ""
	}

	if _, err := w.Write(data); err != nil {
		return data, ""
	}
	if err := w.Close(); err != nil {
		return data, ""
	}

	compressed := buf.Bytes()
	// Only use compressed version if it's actually smaller.
	if len(compressed) >= len(data) {
		return data, ""
	}

	return compressed, "gzip"
}

// DecompressGzip decompresses gzip data.
func DecompressGzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
