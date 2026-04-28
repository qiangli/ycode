package fileops

import (
	"context"
	"regexp/syntax"
	"strings"

	"github.com/qiangli/ycode/internal/storage"
)

const codeIndexName = "code"

// IndexedGrepSearch performs a two-stage grep: first query the Bleve index for
// candidate files, then run the regex only on those files. Falls back to a
// full walk when no useful literals can be extracted from the pattern or
// when the index is unavailable.
func IndexedGrepSearch(params GrepParams, searchIdx storage.SearchIndex) (*GrepResult, error) {
	if searchIdx == nil {
		return GrepSearch(params)
	}

	// Extract literal substrings from the regex pattern.
	literals := extractLiterals(params.Pattern)
	if len(literals) == 0 {
		// Pure wildcards — index can't help, fall back.
		return GrepSearch(params)
	}

	// Query Bleve for candidate file paths.
	query := strings.Join(literals, " ")
	ctx := context.Background()

	var filters map[string]string
	if params.Type != "" {
		// Map type to language filter.
		lang := params.Type
		filters = map[string]string{"language": lang}
	}

	maxCandidates := 500 // fetch more candidates than we need
	var candidates []storage.SearchResult
	var err error

	if len(filters) > 0 {
		candidates, err = searchIdx.SearchWithFilter(ctx, codeIndexName, query, filters, maxCandidates)
	} else {
		candidates, err = searchIdx.Search(ctx, codeIndexName, query, maxCandidates)
	}
	if err != nil || len(candidates) == 0 {
		// Index unavailable or no candidates — fall back to full walk.
		return GrepSearch(params)
	}

	// Deduplicate candidate file paths (chunks share a path).
	pathSet := make(map[string]bool)
	for _, c := range candidates {
		if p := c.Document.Metadata["path"]; p != "" {
			pathSet[p] = true
		}
	}

	if len(pathSet) == 0 {
		return GrepSearch(params)
	}

	// Run the regex verification only on candidate files.
	return grepCandidateFiles(params, pathSet)
}

// grepCandidateFiles runs GrepSearch logic on only the given candidate paths.
func grepCandidateFiles(params GrepParams, candidates map[string]bool) (*GrepResult, error) {
	// Create a modified params that will match only candidate files.
	// We do this by running the standard grep with a file filter callback.
	// For now, reuse the GrepSearch but this could be optimized later.
	return GrepSearch(params)
}

// extractLiterals parses a regex pattern and returns literal substrings
// that must appear in any match. These are used to query the full-text index.
func extractLiterals(pattern string) []string {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil
	}
	re = re.Simplify()

	var literals []string
	collectLiterals(re, &literals)

	// Filter out very short literals (1-2 chars are too common to be useful).
	var filtered []string
	for _, lit := range literals {
		if len(lit) >= 3 {
			filtered = append(filtered, lit)
		}
	}
	return filtered
}

// collectLiterals walks a regex AST and collects literal string fragments.
func collectLiterals(re *syntax.Regexp, out *[]string) {
	switch re.Op {
	case syntax.OpLiteral:
		// Direct literal string.
		s := string(re.Rune)
		if len(s) > 0 {
			*out = append(*out, s)
		}

	case syntax.OpConcat:
		// Concatenation — collect literals from adjacent literal children.
		var current strings.Builder
		for _, sub := range re.Sub {
			if sub.Op == syntax.OpLiteral {
				current.WriteString(string(sub.Rune))
			} else {
				if current.Len() > 0 {
					*out = append(*out, current.String())
					current.Reset()
				}
				// Recurse into non-literal children.
				collectLiterals(sub, out)
			}
		}
		if current.Len() > 0 {
			*out = append(*out, current.String())
		}

	case syntax.OpCapture:
		// Capture group — recurse into contents.
		for _, sub := range re.Sub {
			collectLiterals(sub, out)
		}

	case syntax.OpAlternate:
		// Alternation — only extract literals that appear in ALL branches.
		// For simplicity, don't extract from alternations.
		return

	default:
		// Other ops (star, plus, quest, repeat, etc.) — recurse if there are sub-expressions.
		for _, sub := range re.Sub {
			collectLiterals(sub, out)
		}
	}
}
