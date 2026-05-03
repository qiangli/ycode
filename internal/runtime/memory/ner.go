package memory

import (
	"regexp"
	"strings"
	"unicode"
)

// Entity type constants for enhanced NER extraction.
const (
	EntityTypePerson       = "person"
	EntityTypeProject      = "project"
	EntityTypeTechnology   = "technology"
	EntityTypeOrganization = "organization"
	EntityTypeConcept      = "concept"
	EntityTypeFilePath     = "file_path"
	EntityTypeURL          = "reference"
	EntityTypeCompound     = "compound"
)

// genericHeads are words that are too generic to be entities by themselves.
var genericHeads = map[string]bool{
	"thing": true, "things": true, "way": true, "ways": true,
	"time": true, "times": true, "stuff": true, "information": true,
	"material": true, "work": true, "works": true, "job": true,
	"file": true, "files": true, "code": true, "data": true,
	"issue": true, "issues": true, "problem": true, "problems": true,
	"error": true, "errors": true, "change": true, "changes": true,
	"part": true, "parts": true, "type": true, "types": true,
	"kind": true, "set": true, "list": true, "case": true,
}

// ExtractEntitiesEnhanced extracts entities with richer type detection.
// Supplements the existing ExtractEntities with proper nouns, quoted text,
// Go identifiers, and compound nouns.
func ExtractEntitiesEnhanced(content string) []Entity {
	var entities []Entity
	seen := make(map[string]bool)

	// Start with base extraction (file paths, URLs, Go packages).
	base := ExtractEntities(content)
	for _, e := range base {
		key := strings.ToLower(e.Name)
		if !seen[key] {
			seen[key] = true
			entities = append(entities, e)
		}
	}

	// Proper nouns: capitalized multi-word sequences not at sentence start.
	properNouns := extractProperNouns(content)
	for _, name := range properNouns {
		key := strings.ToLower(name)
		if !seen[key] && len(name) >= 3 {
			seen[key] = true
			entities = append(entities, Entity{Name: name, Type: EntityTypePerson})
		}
	}

	// Quoted text (backticks, single quotes, double quotes).
	quoted := extractQuoted(content)
	for _, q := range quoted {
		key := strings.ToLower(q)
		if !seen[key] && len(q) >= 3 {
			seen[key] = true
			eType := EntityTypeConcept
			// If it looks like a file path or command, classify accordingly.
			if strings.Contains(q, "/") || strings.Contains(q, ".") {
				eType = EntityTypeFilePath
			}
			entities = append(entities, Entity{Name: q, Type: eType})
		}
	}

	// Go/exported identifiers: CamelCase names like MyStruct, HandleRequest.
	goIdents := extractGoIdentifiers(content)
	for _, ident := range goIdents {
		key := strings.ToLower(ident)
		if !seen[key] {
			seen[key] = true
			entities = append(entities, Entity{Name: ident, Type: EntityTypeTechnology})
		}
	}

	return entities
}

// extractProperNouns finds capitalized multi-word sequences not at sentence start.
func extractProperNouns(text string) []string {
	var results []string

	// Split into sentences (crude but sufficient).
	sentences := regexp.MustCompile(`[.!?]\s+`).Split(text, -1)

	for _, sent := range sentences {
		words := strings.Fields(sent)
		if len(words) < 2 {
			continue
		}

		// Skip the first word of each sentence (always capitalized).
		var current []string
		for i := 1; i < len(words); i++ {
			w := strings.Trim(words[i], ".,;:!?\"'()[]{}—–-")
			if w == "" {
				if len(current) > 0 {
					results = append(results, strings.Join(current, " "))
					current = nil
				}
				continue
			}

			first := rune(w[0])
			if unicode.IsUpper(first) && !isCommonWord(w) {
				current = append(current, w)
			} else {
				if len(current) > 0 {
					results = append(results, strings.Join(current, " "))
					current = nil
				}
			}
		}
		if len(current) > 0 {
			results = append(results, strings.Join(current, " "))
		}
	}

	return results
}

// isCommonWord returns true for words that happen to be capitalized but aren't proper nouns.
func isCommonWord(w string) bool {
	lower := strings.ToLower(w)
	common := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "are": true, "was": true, "were": true, "be": true,
		"i": true, "my": true, "we": true, "they": true, "it": true,
		"this": true, "that": true, "these": true, "those": true,
		"if": true, "then": true, "else": true, "when": true,
		"not": true, "no": true, "yes": true, "so": true, "also": true,
	}
	return common[lower]
}

// extractQuoted extracts text in backticks, single quotes, and double quotes.
func extractQuoted(text string) []string {
	var results []string

	// Backtick-quoted.
	btRe := regexp.MustCompile("`([^`]+)`")
	for _, m := range btRe.FindAllStringSubmatch(text, -1) {
		q := strings.TrimSpace(m[1])
		if len(q) >= 3 && !genericHeads[strings.ToLower(q)] {
			results = append(results, q)
		}
	}

	// Double-quoted (only short phrases, not sentences).
	dqRe := regexp.MustCompile(`"([^"]{3,40})"`)
	for _, m := range dqRe.FindAllStringSubmatch(text, -1) {
		q := strings.TrimSpace(m[1])
		if !genericHeads[strings.ToLower(q)] {
			results = append(results, q)
		}
	}

	return results
}

// extractGoIdentifiers finds CamelCase exported Go identifiers.
func extractGoIdentifiers(text string) []string {
	// Match words that start with uppercase and have at least one lowercase after.
	re := regexp.MustCompile(`\b([A-Z][a-z]+(?:[A-Z][a-z]+)+)\b`)
	matches := re.FindAllString(text, -1)

	var results []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if !seen[m] && len(m) >= 4 {
			seen[m] = true
			results = append(results, m)
		}
	}
	return results
}

// EntityBoostAttenuation reduces the boost for entities linked to many memories.
// Inspired by Mem0's attenuation formula: 1 / (1 + 0.001 * (n-1)^2).
func EntityBoostAttenuation(linkedCount int) float64 {
	if linkedCount <= 1 {
		return 1.0
	}
	n := float64(linkedCount - 1)
	return 1.0 / (1.0 + 0.001*n*n)
}
