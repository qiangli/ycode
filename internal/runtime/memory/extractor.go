package memory

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ExtractedFact represents a single fact extracted from a conversation turn.
type ExtractedFact struct {
	Text           string   `json:"text"`
	AttributedTo   string   `json:"attributed_to"`      // "user" or "assistant"
	TemporalAnchor string   `json:"temporal_anchor"`    // resolved date if temporal reference found
	Entities       []string `json:"entities,omitempty"` // extracted entity names
	Confidence     float64  `json:"confidence"`         // 0-1
}

// ExtractionContext holds the inputs for memory extraction.
type ExtractionContext struct {
	NewMessages     []ExtractionMessage // current turn messages
	RecentMessages  []ExtractionMessage // last N messages for pronoun resolution
	ExistingHashes  map[string]bool     // MD5 hashes of existing memories for dedup
	ObservationDate time.Time
}

// ExtractionMessage is a simplified message for extraction context.
type ExtractionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MemoryExtractor extracts memorable facts from conversation turns using an LLM.
// Uses ADD-only semantics inspired by Mem0's v3 pipeline.
type MemoryExtractor struct {
	// LLMFunc calls an LLM with system + user prompts.
	// Returns the LLM's response text.
	LLMFunc func(system, user string) (string, error)

	manager *Manager
	logger  *slog.Logger
}

// NewMemoryExtractor creates a memory extractor.
func NewMemoryExtractor(manager *Manager) *MemoryExtractor {
	return &MemoryExtractor{
		manager: manager,
		logger:  slog.Default(),
	}
}

// Extract analyzes conversation messages and returns extracted facts.
// This is the core extraction pipeline (simplified from Mem0's 8-phase approach).
func (e *MemoryExtractor) Extract(ctx ExtractionContext) ([]ExtractedFact, error) {
	if e.LLMFunc == nil {
		return nil, fmt.Errorf("extractor: LLMFunc not configured")
	}

	if len(ctx.NewMessages) == 0 {
		return nil, nil
	}

	// Phase 1: Retrieve existing memories for dedup context.
	var existingSnippets []string
	summary := summarizeMessages(ctx.NewMessages)
	if summary != "" {
		results, _ := e.manager.Recall(summary, 10)
		for i, r := range results {
			existingSnippets = append(existingSnippets,
				fmt.Sprintf("[%d] %s: %s", i, r.Memory.Name, r.Memory.Description))
		}
	}

	// Phase 2: Call LLM with extraction prompt.
	systemPrompt := buildExtractionSystemPrompt()
	userPrompt := buildExtractionUserPrompt(ctx, existingSnippets)

	response, err := e.LLMFunc(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("extractor: LLM call failed: %w", err)
	}

	// Phase 3: Parse response.
	facts, err := parseExtractionResponse(response)
	if err != nil {
		e.logger.Warn("extractor: failed to parse LLM response", "error", err)
		return nil, nil
	}

	// Phase 4: Dedup against existing memory hashes.
	var surviving []ExtractedFact
	for _, fact := range facts {
		if fact.Text == "" || fact.Confidence < 0.3 {
			continue
		}

		hash := ContentHash(fact.Text)
		if ctx.ExistingHashes[hash] {
			continue // exact duplicate
		}

		surviving = append(surviving, fact)
	}

	return surviving, nil
}

// PersistFacts saves extracted facts as episodic memories.
func (e *MemoryExtractor) PersistFacts(facts []ExtractedFact) int {
	saved := 0
	for _, fact := range facts {
		name := generateMemoryName(fact.Text)
		mem := &Memory{
			Name:        name,
			Description: fact.Text,
			Type:        TypeEpisodic,
			Content:     fact.Text,
			Importance:  fact.Confidence,
			ContentHash: ContentHash(fact.Text),
			Entities:    fact.Entities,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := e.manager.Save(mem); err != nil {
			e.logger.Warn("extractor: failed to save fact", "error", err)
			continue
		}
		saved++
	}
	return saved
}

// ContentHash returns the MD5 hash of normalized text for deduplication.
func ContentHash(text string) string {
	normalized := NormalizeForHash(text)
	hash := md5.Sum([]byte(normalized))
	return fmt.Sprintf("%x", hash)
}

// NormalizeForHash normalizes text for content-hash comparison.
func NormalizeForHash(text string) string {
	text = strings.ToLower(text)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// generateMemoryName creates a short name from fact text.
func generateMemoryName(text string) string {
	words := strings.Fields(text)
	if len(words) > 6 {
		words = words[:6]
	}
	name := strings.Join(words, "_")
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r - 'A' + 'a'
		}
		return '_'
	}, name)
	// Truncate.
	if len(name) > 50 {
		name = name[:50]
	}
	// Add short hash for uniqueness.
	hash := md5.Sum([]byte(text))
	return fmt.Sprintf("%s_%x", name, hash[:3])
}

// summarizeMessages creates a brief summary of messages for recall queries.
func summarizeMessages(msgs []ExtractionMessage) string {
	var parts []string
	for _, m := range msgs {
		content := m.Content
		if len(content) > 200 {
			content = content[:200]
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, " ")
}

// parseExtractionResponse parses the LLM's JSON response into extracted facts.
func parseExtractionResponse(response string) ([]ExtractedFact, error) {
	// Try to extract JSON from the response.
	jsonStr := extractJSONFromResponse(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result struct {
		Facts []ExtractedFact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return result.Facts, nil
}

// extractJSONFromResponse extracts a JSON object from a response that may contain markdown.
func extractJSONFromResponse(s string) string {
	s = strings.TrimSpace(s)

	// Try raw JSON first.
	if strings.HasPrefix(s, "{") {
		return s
	}

	// Try code fences.
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		end := strings.Index(s[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		end := strings.Index(s[start:], "```")
		if end >= 0 {
			inner := strings.TrimSpace(s[start : start+end])
			if strings.HasPrefix(inner, "{") {
				return inner
			}
		}
	}

	// Try to find JSON object anywhere in the text.
	if start := strings.Index(s, "{"); start >= 0 {
		depth := 0
		for i := start; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return s[start : i+1]
				}
			}
		}
	}

	return ""
}
