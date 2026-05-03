package fileops

import (
	"testing"
)

func TestDetectEncoding_UTF8(t *testing.T) {
	data := []byte("hello world")
	if enc := DetectEncoding(data); enc != EncodingUTF8 {
		t.Errorf("expected UTF-8, got %s", enc)
	}
}

func TestDetectEncoding_UTF8BOM(t *testing.T) {
	data := []byte{0xEF, 0xBB, 0xBF, 'h', 'e', 'l', 'l', 'o'}
	if enc := DetectEncoding(data); enc != EncodingUTF8BOM {
		t.Errorf("expected UTF-8 BOM, got %s", enc)
	}
}

func TestDetectEncoding_UTF16LE(t *testing.T) {
	data := []byte{0xFF, 0xFE, 'h', 0, 'e', 0}
	if enc := DetectEncoding(data); enc != EncodingUTF16LE {
		t.Errorf("expected UTF-16LE, got %s", enc)
	}
}

func TestDetectEncoding_UTF16BE(t *testing.T) {
	data := []byte{0xFE, 0xFF, 0, 'h', 0, 'e'}
	if enc := DetectEncoding(data); enc != EncodingUTF16BE {
		t.Errorf("expected UTF-16BE, got %s", enc)
	}
}

func TestDetectEncoding_Empty(t *testing.T) {
	if enc := DetectEncoding(nil); enc != EncodingUTF8 {
		t.Errorf("empty should default to UTF-8, got %s", enc)
	}
}

func TestDetectLineEndings_LF(t *testing.T) {
	data := []byte("line1\nline2\nline3\n")
	if le := DetectLineEndings(data); le != LineEndingLF {
		t.Errorf("expected LF, got %s", le)
	}
}

func TestDetectLineEndings_CRLF(t *testing.T) {
	data := []byte("line1\r\nline2\r\nline3\r\n")
	if le := DetectLineEndings(data); le != LineEndingCRLF {
		t.Errorf("expected CRLF, got %s", le)
	}
}

func TestDetectLineEndings_CR(t *testing.T) {
	data := []byte("line1\rline2\rline3\r")
	if le := DetectLineEndings(data); le != LineEndingCR {
		t.Errorf("expected CR, got %s", le)
	}
}

func TestDetectLineEndings_Mixed(t *testing.T) {
	// Mostly CRLF with one LF — should detect CRLF.
	data := []byte("line1\r\nline2\r\nline3\nline4\r\n")
	if le := DetectLineEndings(data); le != LineEndingCRLF {
		t.Errorf("expected CRLF (majority), got %s", le)
	}
}

func TestNormalizeLineEndings(t *testing.T) {
	input := "line1\r\nline2\nline3\r"

	// To LF.
	lf := NormalizeLineEndings(input, LineEndingLF)
	if lf != "line1\nline2\nline3\n" {
		t.Errorf("normalize to LF failed: %q", lf)
	}

	// To CRLF.
	crlf := NormalizeLineEndings(input, LineEndingCRLF)
	if crlf != "line1\r\nline2\r\nline3\r\n" {
		t.Errorf("normalize to CRLF failed: %q", crlf)
	}
}

func TestStripBOM(t *testing.T) {
	withBOM := []byte{0xEF, 0xBB, 0xBF, 'h', 'e', 'l', 'l', 'o'}
	stripped := StripBOM(withBOM)
	if string(stripped) != "hello" {
		t.Errorf("expected 'hello', got %q", string(stripped))
	}

	withoutBOM := []byte("hello")
	stripped = StripBOM(withoutBOM)
	if string(stripped) != "hello" {
		t.Error("should not modify data without BOM")
	}
}

func TestEncodingString(t *testing.T) {
	if EncodingUTF8.String() != "utf-8" {
		t.Error("wrong string for UTF-8")
	}
	if EncodingCRLF := LineEndingCRLF; EncodingCRLF.String() != "crlf" {
		t.Error("wrong string for CRLF")
	}
}

func TestLineEndingSeparator(t *testing.T) {
	if LineEndingLF.Separator() != "\n" {
		t.Error("LF separator wrong")
	}
	if LineEndingCRLF.Separator() != "\r\n" {
		t.Error("CRLF separator wrong")
	}
}
