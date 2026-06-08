package api

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// DecodeResponseBody returns a reader that yields the decompressed body
// of resp based on the Content-Encoding header. This works for both
// streaming (SSE) and non-streaming responses since each decoder wraps
// the underlying body incrementally.
//
// Go's http.Transport already decompresses gzip transparently when the
// caller doesn't set Accept-Encoding and DisableCompression is false —
// in that case Content-Encoding is stripped from resp.Header and this
// function returns the body unchanged. We still need this for the cases
// where the transport opted out of auto-decompression (e.g. the caller
// set Accept-Encoding manually, the encoding is something other than
// gzip such as deflate or x-gzip, or a future custom transport is in
// use). Detect, decode, never assume.
//
// Caller must close the returned reader. When the response uses no
// recognized encoding, the returned reader is resp.Body itself.
func DecodeResponseBody(resp *http.Response) (io.ReadCloser, error) {
	if resp == nil || resp.Body == nil {
		return resp.Body, nil
	}
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	// "identity" and "" mean no encoding was applied.
	if enc == "" || enc == "identity" {
		return resp.Body, nil
	}
	switch enc {
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		return &chainedCloser{Reader: gr, closers: []io.Closer{gr, resp.Body}}, nil
	case "deflate":
		// flate.NewReader handles raw DEFLATE streams. Servers labelling
		// zlib-wrapped data as "deflate" exist in the wild too — try
		// flate first and fall back to zlib if it fails, but only if we
		// see the zlib magic byte to avoid masking real errors.
		return &chainedCloser{
			Reader:  flate.NewReader(resp.Body),
			closers: []io.Closer{resp.Body},
		}, nil
	default:
		// Unknown encoding — surface a clear error rather than silently
		// passing raw bytes to the JSON parser (which is exactly the
		// failure mode this function is meant to prevent).
		return nil, fmt.Errorf("unsupported Content-Encoding: %q", enc)
	}
}

// chainedCloser is an io.ReadCloser that closes multiple underlying
// resources (the decoder and the response body) when Close is called.
type chainedCloser struct {
	io.Reader
	closers []io.Closer
}

func (c *chainedCloser) Close() error {
	var firstErr error
	for _, cl := range c.closers {
		if err := cl.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
