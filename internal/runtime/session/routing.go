package session

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
)

// ContentRoute determines how a tool result's content should be handled during pruning.
type ContentRoute string

const (
	// RouteFull keeps the content verbatim.
	RouteFull ContentRoute = "full"
	// RoutePartial keeps head + tail with omission marker.
	RoutePartial ContentRoute = "partial"
	// RouteSummary replaces content with a one-line description.
	RouteSummary ContentRoute = "summary"
	// RouteExcluded drops the content entirely (keeps a placeholder).
	RouteExcluded ContentRoute = "excluded"
)

// RoutingCache caches content routing decisions by content hash.
type RoutingCache struct {
	mu    sync.RWMutex
	cache map[string]ContentRoute
}

// NewRoutingCache creates an empty routing cache.
func NewRoutingCache() *RoutingCache {
	return &RoutingCache{
		cache: make(map[string]ContentRoute),
	}
}

// Get retrieves a cached routing decision.
func (rc *RoutingCache) Get(contentHash string) (ContentRoute, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	route, ok := rc.cache[contentHash]
	return route, ok
}

// Set stores a routing decision.
func (rc *RoutingCache) Set(contentHash string, route ContentRoute) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cache[contentHash] = route
}

// RouteContent determines the routing for a tool result based on heuristics.
// When aggressive is true, thresholds are tighter for non-caching providers.
func RouteContent(toolName string, content string, isError bool, cache *RoutingCache, aggressive bool) ContentRoute {
	hash := contentHash(content)

	// Check cache first.
	if cache != nil {
		if route, ok := cache.Get(hash); ok {
			return route
		}
	}

	route := classifyContent(toolName, content, isError, aggressive)

	if cache != nil {
		cache.Set(hash, route)
	}
	return route
}

// classifyContent applies heuristic rules to determine routing.
// When aggressive is true, thresholds are tighter for non-caching providers.
func classifyContent(toolName string, content string, isError bool, aggressive bool) ContentRoute {
	// Error outputs are always kept in full — they contain critical diagnostics.
	if isError {
		return RouteFull
	}

	// Read operations — keep full only if small, partial otherwise.
	readTools := map[string]bool{
		"read_file": true, "read_multiple_files": true,
	}
	if readTools[toolName] {
		threshold := 2000
		if aggressive {
			threshold = 1000
		}
		if len(content) < threshold {
			return RouteFull
		}
		return RoutePartial
	}

	// Write/edit confirmations — short and important.
	writeTools := map[string]bool{
		"write_file": true, "edit_file": true,
	}
	if writeTools[toolName] {
		return RouteFull
	}

	// Search results — can be large, partial is fine.
	searchTools := map[string]bool{
		"glob_search": true, "grep_search": true,
	}
	if searchTools[toolName] {
		threshold := 500
		if aggressive {
			threshold = 300
		}
		if len(content) < threshold {
			return RouteFull
		}
		return RoutePartial
	}

	// Bash outputs — varies widely.
	if toolName == "bash" {
		lower := strings.ToLower(content)
		// Test results and build output — keep partial for diagnostics.
		if strings.Contains(lower, "pass") || strings.Contains(lower, "fail") ||
			strings.Contains(lower, "error") || strings.Contains(lower, "warning") {
			return RoutePartial
		}
		// Non-diagnostic output: summarize when large.
		summaryThreshold := 1000
		if aggressive {
			summaryThreshold = 500
		}
		if len(content) > summaryThreshold {
			return RouteSummary
		}
		return RoutePartial
	}

	// Default: partial for anything large, full for small.
	defaultThreshold := 2000
	if aggressive {
		defaultThreshold = 1000
	}
	if len(content) > defaultThreshold {
		return RoutePartial
	}
	return RouteFull
}

// ApplyRoute transforms content according to the routing decision.
func ApplyRoute(content string, route ContentRoute, toolName string) string {
	switch route {
	case RouteFull:
		return content
	case RoutePartial:
		return partialContent(content, SoftTrimHeadChars, SoftTrimTailChars)
	case RouteSummary:
		return summarizeContent(content, toolName)
	case RouteExcluded:
		return "[Content excluded during context pruning. Re-run the tool if needed.]"
	default:
		return content
	}
}

// partialContent keeps head and tail characters with an omission marker.
func partialContent(content string, headChars, tailChars int) string {
	if len(content) <= headChars+tailChars+50 {
		return content
	}

	head := content[:headChars]
	tail := content[len(content)-tailChars:]
	omitted := len(content) - headChars - tailChars

	return head + fmt.Sprintf("\n[... %d characters omitted ...]\n", omitted) + tail
}

// summarizeContent produces a one-line summary of the content.
func summarizeContent(content string, toolName string) string {
	lines := strings.Count(content, "\n") + 1
	chars := len(content)
	firstLine := content
	if idx := strings.Index(content, "\n"); idx >= 0 {
		firstLine = content[:idx]
	}
	if len(firstLine) > 100 {
		firstLine = firstLine[:100] + "..."
	}

	return fmt.Sprintf("[%s output: %d lines, %d chars. First line: %s]", toolName, lines, chars, firstLine)
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}
