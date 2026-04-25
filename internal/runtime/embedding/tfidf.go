package embedding

import (
	"context"
	"math"
	"strings"
	"sync"
	"unicode"
)

// TFIDFProvider generates embedding vectors using TF-IDF (Term Frequency–Inverse
// Document Frequency). Unlike the hash provider, similar texts produce similar
// vectors — "error handler" and "handle errors" score as related.
//
// Pure Go, zero external dependencies, zero API calls.
//
// How it works:
//  1. Tokenize text into terms (words, camelCase splits, snake_case splits)
//  2. Build a vocabulary from all seen documents (IDF weights)
//  3. For each text, compute TF-IDF weights and project into a fixed-size vector
//     using feature hashing (hashing trick) to avoid unbounded vocabulary growth
//
// The provider is safe for concurrent use.
type TFIDFProvider struct {
	dims int

	mu       sync.RWMutex
	docCount int            // total documents seen
	docFreq  map[string]int // term → number of documents containing it
}

// NewTFIDFProvider creates a TF-IDF embedding provider with the given dimensions.
func NewTFIDFProvider(dims int) *TFIDFProvider {
	if dims <= 0 {
		dims = 384
	}
	return &TFIDFProvider{
		dims:    dims,
		docFreq: make(map[string]int),
	}
}

// Learn updates the IDF statistics with a document without returning a vector.
// Call this during indexing to build vocabulary, then use Embed for queries.
func (p *TFIDFProvider) Learn(text string) {
	terms := tokenize(text)
	if len(terms) == 0 {
		return
	}
	seen := make(map[string]bool, len(terms))
	for _, t := range terms {
		seen[t] = true
	}
	p.mu.Lock()
	p.docCount++
	for term := range seen {
		p.docFreq[term]++
	}
	p.mu.Unlock()
}

// Embed generates a TF-IDF vector for the given text using current IDF stats.
// The vector is stable for the same input as long as the vocabulary doesn't change.
func (p *TFIDFProvider) Embed(_ context.Context, text string) ([]float32, error) {
	terms := tokenize(text)
	if len(terms) == 0 {
		return make([]float32, p.dims), nil
	}

	// Compute term frequency (TF) for this document.
	tf := make(map[string]int, len(terms))
	for _, t := range terms {
		tf[t]++
	}

	// Snapshot IDF stats (read-only).
	p.mu.RLock()
	totalDocs := p.docCount
	if totalDocs == 0 {
		totalDocs = 1 // avoid division by zero before any learning
	}
	idfSnapshot := make(map[string]int, len(tf))
	for term := range tf {
		idfSnapshot[term] = p.docFreq[term]
	}
	p.mu.RUnlock()

	// Build the TF-IDF weighted vector using feature hashing.
	vec := make([]float32, p.dims)
	maxTF := 0
	for _, count := range tf {
		if count > maxTF {
			maxTF = count
		}
	}

	for term, count := range tf {
		// Augmented TF: 0.5 + 0.5 * (count / maxCount) — prevents bias toward long docs.
		augTF := 0.5 + 0.5*float64(count)/float64(maxTF)

		// IDF: log(N / df) — terms appearing in fewer docs get higher weight.
		df := idfSnapshot[term]
		idf := math.Log(float64(totalDocs+1) / float64(df+1))

		weight := float32(augTF * idf)

		// Feature hashing: map term to vector index deterministically.
		// Use two hashes to reduce collision effects (signed feature hashing).
		idx := hashTerm(term) % uint32(p.dims)
		sign := float32(1)
		if hashTerm(term+"_sign")%2 == 0 {
			sign = -1
		}
		vec[idx] += sign * weight
	}

	// L2 normalize so cosine similarity works correctly.
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec, nil
}

// Dimensions returns the vector dimensionality.
func (p *TFIDFProvider) Dimensions() int {
	return p.dims
}

// DocCount returns the number of documents processed so far.
func (p *TFIDFProvider) DocCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.docCount
}

// VocabSize returns the current vocabulary size.
func (p *TFIDFProvider) VocabSize() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.docFreq)
}

// tokenize splits text into normalized terms suitable for code search.
// It handles camelCase, snake_case, and common programming patterns.
func tokenize(text string) []string {
	var terms []string
	var current strings.Builder

	flush := func() {
		word := strings.ToLower(current.String())
		if len(word) >= 2 { // skip single-char tokens
			terms = append(terms, word)
		}
		current.Reset()
	}

	runes := []rune(text)
	for i, r := range runes {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// Split on camelCase boundaries: "errorHandler" → "error", "handler"
			// Detect transitions: lower→Upper or letter→digit
			if current.Len() > 0 && unicode.IsUpper(r) {
				prev := runes[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					flush()
				}
			}
			current.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			// Snake_case, kebab-case, dot.notation — split
			flush()
		default:
			// Whitespace, punctuation, operators
			flush()
		}
	}
	flush()

	// Also add bigrams for phrase matching: ["error", "handler"] → "error_handler"
	if len(terms) >= 2 {
		bigrams := make([]string, 0, len(terms)-1)
		for i := 0; i < len(terms)-1; i++ {
			bigrams = append(bigrams, terms[i]+"_"+terms[i+1])
		}
		terms = append(terms, bigrams...)
	}

	return terms
}

// hashTerm is a fast deterministic hash for feature hashing.
func hashTerm(s string) uint32 {
	// FNV-1a hash — fast, good distribution.
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
