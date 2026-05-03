package fileops

import (
	"fmt"
	"os"
	"strings"
)

// EditFileParams configures a text replacement edit.
type EditFileParams struct {
	Path       string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// EditFile performs a string replacement in a file.
// Preserves the file's BOM and line ending style in replacement text.
func EditFile(params EditFileParams) error {
	data, err := os.ReadFile(params.Path)
	if err != nil {
		return fmt.Errorf("read %s: %w", params.Path, err)
	}

	content := string(data)

	if params.OldString == params.NewString {
		return fmt.Errorf("old_string and new_string are identical")
	}

	// Detect file's line ending style and normalize replacement to match.
	le := DetectLineEndings(data)
	newString := NormalizeLineEndings(params.NewString, le)
	oldString := params.OldString

	// Strip BOM from content for matching, but preserve it on write.
	var bomPrefix string
	enc := DetectEncoding(data)
	if enc == EncodingUTF8BOM {
		bomPrefix = "\xEF\xBB\xBF"
		content = content[3:] // Strip BOM for matching.
	}

	if !strings.Contains(content, oldString) {
		// Fuzzy fallback: try line-trimmed and block-anchor matching.
		match := FindFuzzyMatch(content, oldString)
		if match == nil {
			return fmt.Errorf("old_string not found in %s (tried exact and fuzzy matching). Use read_file to verify current content, or grep_search to find similar text", params.Path)
		}
		// Apply fuzzy-matched replacement.
		content = content[:match.StartByte] + newString + content[match.EndByte:]
		return os.WriteFile(params.Path, []byte(bomPrefix+content), 0o644)
	}

	if params.ReplaceAll {
		content = strings.ReplaceAll(content, oldString, newString)
	} else {
		// Ensure uniqueness for single replacement.
		count := strings.Count(content, oldString)
		if count > 1 {
			return fmt.Errorf("old_string appears %d times in %s; use replace_all or provide more context", count, params.Path)
		}
		content = strings.Replace(content, oldString, newString, 1)
	}

	if err := os.WriteFile(params.Path, []byte(bomPrefix+content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", params.Path, err)
	}

	return nil
}
