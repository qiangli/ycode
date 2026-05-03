package fileops

import (
	"bytes"
	"strings"
)

// Encoding represents a detected file encoding.
type Encoding int

const (
	EncodingUTF8 Encoding = iota
	EncodingUTF8BOM
	EncodingUTF16LE
	EncodingUTF16BE
	EncodingLatin1
)

// String returns the encoding name.
func (e Encoding) String() string {
	switch e {
	case EncodingUTF8:
		return "utf-8"
	case EncodingUTF8BOM:
		return "utf-8-bom"
	case EncodingUTF16LE:
		return "utf-16le"
	case EncodingUTF16BE:
		return "utf-16be"
	case EncodingLatin1:
		return "latin-1"
	default:
		return "utf-8"
	}
}

// LineEnding represents a detected line ending style.
type LineEnding int

const (
	LineEndingLF   LineEnding = iota // Unix: \n
	LineEndingCRLF                   // Windows: \r\n
	LineEndingCR                     // Classic Mac: \r
)

// String returns the line ending representation.
func (le LineEnding) String() string {
	switch le {
	case LineEndingLF:
		return "lf"
	case LineEndingCRLF:
		return "crlf"
	case LineEndingCR:
		return "cr"
	default:
		return "lf"
	}
}

// Separator returns the actual line ending characters.
func (le LineEnding) Separator() string {
	switch le {
	case LineEndingCRLF:
		return "\r\n"
	case LineEndingCR:
		return "\r"
	default:
		return "\n"
	}
}

// DetectEncoding examines the first bytes of data to determine the file encoding.
// Checks for BOM markers first, then heuristic analysis.
func DetectEncoding(data []byte) Encoding {
	if len(data) == 0 {
		return EncodingUTF8
	}

	// Check for BOM markers.
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return EncodingUTF8BOM
	}
	if len(data) >= 2 {
		if data[0] == 0xFF && data[1] == 0xFE {
			return EncodingUTF16LE
		}
		if data[0] == 0xFE && data[1] == 0xFF {
			return EncodingUTF16BE
		}
	}

	// Check for null bytes suggesting UTF-16 without BOM.
	if len(data) >= 4 {
		nullCount := 0
		for i := 0; i < min(len(data), 100); i++ {
			if data[i] == 0 {
				nullCount++
			}
		}
		// Significant null bytes suggest UTF-16.
		if nullCount > 10 {
			// Even positions → LE, odd positions → BE.
			evenNull, oddNull := 0, 0
			for i := 0; i < min(len(data), 100); i++ {
				if data[i] == 0 {
					if i%2 == 0 {
						evenNull++
					} else {
						oddNull++
					}
				}
			}
			if oddNull > evenNull {
				return EncodingUTF16LE
			}
			return EncodingUTF16BE
		}
	}

	// Check for high bytes suggesting Latin-1 (0x80-0xFF without valid UTF-8 sequences).
	hasHighBytes := false
	for _, b := range data[:min(len(data), 1000)] {
		if b >= 0x80 {
			hasHighBytes = true
			break
		}
	}
	if hasHighBytes {
		// Try to validate as UTF-8.
		if !isValidUTF8(data[:min(len(data), 1000)]) {
			return EncodingLatin1
		}
	}

	return EncodingUTF8
}

// isValidUTF8 checks if data is valid UTF-8.
func isValidUTF8(data []byte) bool {
	for i := 0; i < len(data); {
		if data[i] < 0x80 {
			i++
			continue
		}
		size := 0
		switch {
		case data[i]&0xE0 == 0xC0:
			size = 2
		case data[i]&0xF0 == 0xE0:
			size = 3
		case data[i]&0xF8 == 0xF0:
			size = 4
		default:
			return false
		}
		if i+size > len(data) {
			return false
		}
		for j := 1; j < size; j++ {
			if data[i+j]&0xC0 != 0x80 {
				return false
			}
		}
		i += size
	}
	return true
}

// DetectLineEndings examines data to determine the dominant line ending style.
// Counts occurrences of each style and returns the majority.
func DetectLineEndings(data []byte) LineEnding {
	crlfCount := bytes.Count(data, []byte("\r\n"))
	crOnly := 0
	lfOnly := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				i++ // Skip the \n of \r\n.
			} else {
				crOnly++
			}
		} else if data[i] == '\n' {
			lfOnly++
		}
	}

	if crlfCount > lfOnly && crlfCount > crOnly {
		return LineEndingCRLF
	}
	if crOnly > lfOnly {
		return LineEndingCR
	}
	return LineEndingLF
}

// NormalizeLineEndings converts all line endings in text to the specified style.
func NormalizeLineEndings(text string, target LineEnding) string {
	// First normalize everything to LF.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	// Then convert to target.
	if target == LineEndingCRLF {
		return strings.ReplaceAll(normalized, "\n", "\r\n")
	}
	if target == LineEndingCR {
		return strings.ReplaceAll(normalized, "\n", "\r")
	}
	return normalized
}

// StripBOM removes a UTF-8 BOM from data if present.
func StripBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}
