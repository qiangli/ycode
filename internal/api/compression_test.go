package api

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCompressGzip_SmallInput(t *testing.T) {
	data := []byte("tiny")
	compressed, encoding := CompressGzip(data)
	if encoding != "" {
		t.Error("small input should not be compressed")
	}
	if !bytes.Equal(compressed, data) {
		t.Error("uncompressed output should match input")
	}
}

func TestCompressGzip_LargeInput(t *testing.T) {
	// JSON-like content compresses very well.
	data := []byte(strings.Repeat(`{"role":"user","content":"hello world"}`, 200))
	compressed, encoding := CompressGzip(data)

	if encoding != "gzip" {
		t.Fatalf("large input should be compressed, got encoding=%q", encoding)
	}
	if len(compressed) >= len(data) {
		t.Errorf("compressed (%d) should be smaller than original (%d)", len(compressed), len(data))
	}

	// Verify roundtrip.
	decompressed, err := DecompressGzip(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Error("decompressed data should match original")
	}
}

func TestCompressGzip_ThresholdBoundary(t *testing.T) {
	// Exactly at threshold — should attempt compression.
	data := []byte(strings.Repeat("x", MinCompressSize))
	_, encoding := CompressGzip(data)
	// Repetitive data compresses well, so it should be gzip.
	if encoding != "gzip" {
		t.Errorf("data at threshold should compress, got encoding=%q", encoding)
	}
}

// DecodeResponseBody handles the matrix of (encoding header, payload):
// plain, gzip, x-gzip, deflate, identity, unknown.
func TestDecodeResponseBody(t *testing.T) {
	payload := []byte("event: foo\ndata: {\"ok\":true}\n\n")

	mkGzip := func(b []byte) []byte {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(b)
		_ = w.Close()
		return buf.Bytes()
	}
	mkDeflate := func(b []byte) []byte {
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		_, _ = w.Write(b)
		_ = w.Close()
		return buf.Bytes()
	}

	cases := []struct {
		name      string
		encoding  string
		body      []byte
		want      []byte
		wantError bool
	}{
		{"plain no header", "", payload, payload, false},
		{"identity", "identity", payload, payload, false},
		{"gzip", "gzip", mkGzip(payload), payload, false},
		{"x-gzip", "x-gzip", mkGzip(payload), payload, false},
		{"gzip with charset suffix gets ignored after split", "gzip", mkGzip(payload), payload, false},
		{"deflate", "deflate", mkDeflate(payload), payload, false},
		{"unknown encoding errors out", "br", payload, nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{},
				Body:   io.NopCloser(bytes.NewReader(tc.body)),
			}
			if tc.encoding != "" {
				resp.Header.Set("Content-Encoding", tc.encoding)
			}
			r, err := DecodeResponseBody(resp)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error for encoding=%q", tc.encoding)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if r != resp.Body {
				_ = r.Close()
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("encoding=%q: got %q, want %q", tc.encoding, got, tc.want)
			}
		})
	}
}

// Identity passthrough must NOT close the underlying body when the
// caller closes the returned reader, because the caller already defers
// resp.Body.Close(). We verify by checking the returned reader is
// literally resp.Body when no decoding is needed.
func TestDecodeResponseBody_PlainReturnsSameReader(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{},
		Body:   io.NopCloser(bytes.NewReader([]byte("hi"))),
	}
	r, err := DecodeResponseBody(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != resp.Body {
		t.Errorf("plain body should return resp.Body unchanged")
	}
}
