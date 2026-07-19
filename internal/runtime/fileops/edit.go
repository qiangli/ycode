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
		// old_string is not a byte-exact match. DO NOT guess the region: silently
		// applying a fuzzy match can rewrite the WRONG location when old_string was
		// built from stale or remembered context — the single biggest way to
		// corrupt a file with no error. Surface the near-miss (if any) and require
		// an exact old_string re-read from current content, mirroring Claude Code.
		if match := FindFuzzyMatch(content, oldString); match != nil {
			return fmt.Errorf("old_string is not an EXACT match in %s — a SIMILAR block exists (near byte %d), but I will not guess the region. Re-read the file with read_file and copy old_string verbatim from its CURRENT content (whitespace and all)", params.Path, match.StartByte)
		}
		return fmt.Errorf("old_string not found in %s. Use read_file to see the current content, or grep_search to locate similar text, then copy old_string verbatim", params.Path)
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
