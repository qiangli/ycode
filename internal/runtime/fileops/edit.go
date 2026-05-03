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
func EditFile(params EditFileParams) error {
	data, err := os.ReadFile(params.Path)
	if err != nil {
		return fmt.Errorf("read %s: %w", params.Path, err)
	}

	content := string(data)

	if params.OldString == params.NewString {
		return fmt.Errorf("old_string and new_string are identical")
	}

	if !strings.Contains(content, params.OldString) {
		// Fuzzy fallback: try line-trimmed and block-anchor matching.
		match := FindFuzzyMatch(content, params.OldString)
		if match == nil {
			return fmt.Errorf("old_string not found in %s (tried exact and fuzzy matching). Use read_file to verify current content, or grep_search to find similar text", params.Path)
		}
		// Apply fuzzy-matched replacement.
		content = content[:match.StartByte] + params.NewString + content[match.EndByte:]
		return os.WriteFile(params.Path, []byte(content), 0o644)
	}

	if params.ReplaceAll {
		content = strings.ReplaceAll(content, params.OldString, params.NewString)
	} else {
		// Ensure uniqueness for single replacement.
		count := strings.Count(content, params.OldString)
		if count > 1 {
			return fmt.Errorf("old_string appears %d times in %s; use replace_all or provide more context", count, params.Path)
		}
		content = strings.Replace(content, params.OldString, params.NewString, 1)
	}

	if err := os.WriteFile(params.Path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", params.Path, err)
	}

	return nil
}
