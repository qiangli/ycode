package session

import (
	"strings"
	"testing"
)

func TestRouteContent_ErrorAlwaysFull(t *testing.T) {
	cache := NewRoutingCache()
	route := RouteContent("bash", strings.Repeat("x", 5000), true, cache, false)
	if route != RouteFull {
		t.Errorf("error outputs should always be RouteFull, got %s", route)
	}
}

func TestRouteContent_ReadFileSmall(t *testing.T) {
	route := RouteContent("read_file", "small content", false, nil, false)
	if route != RouteFull {
		t.Errorf("small read_file should be RouteFull, got %s", route)
	}
}

func TestRouteContent_ReadFileLarge(t *testing.T) {
	route := RouteContent("read_file", strings.Repeat("x", 3000), false, nil, false)
	if route != RoutePartial {
		t.Errorf("large read_file should be RoutePartial, got %s", route)
	}
}

func TestRouteContent_WriteFileFull(t *testing.T) {
	route := RouteContent("write_file", "wrote 500 bytes to file.go", false, nil, false)
	if route != RouteFull {
		t.Errorf("write_file should be RouteFull, got %s", route)
	}
}

func TestRouteContent_LargeGrepPartial(t *testing.T) {
	route := RouteContent("grep_search", strings.Repeat("match\n", 500), false, nil, false)
	if route != RoutePartial {
		t.Errorf("large grep should be RoutePartial, got %s", route)
	}
}

func TestRouteContent_CacheHit(t *testing.T) {
	cache := NewRoutingCache()
	content := "test content"

	// First call classifies.
	route1 := RouteContent("bash", content, false, cache, false)
	// Second call should hit cache.
	route2 := RouteContent("bash", content, false, cache, false)

	if route1 != route2 {
		t.Errorf("cache should return same route: %s vs %s", route1, route2)
	}
}

func TestApplyRoute_Full(t *testing.T) {
	content := "full content here"
	result := ApplyRoute(content, RouteFull, "bash")
	if result != content {
		t.Error("RouteFull should return content unchanged")
	}
}

func TestApplyRoute_Partial(t *testing.T) {
	content := strings.Repeat("x", 2000)
	result := ApplyRoute(content, RoutePartial, "bash")
	if !strings.Contains(result, "characters omitted") {
		t.Error("RoutePartial should contain omission marker")
	}
	if len(result) >= len(content) {
		t.Error("RoutePartial should be shorter than original")
	}
}

func TestApplyRoute_Summary(t *testing.T) {
	content := strings.Repeat("line\n", 100)
	result := ApplyRoute(content, RouteSummary, "bash")
	if !strings.Contains(result, "bash output") {
		t.Error("RouteSummary should contain tool name")
	}
	if !strings.Contains(result, "lines") {
		t.Error("RouteSummary should contain line count")
	}
}

func TestApplyRoute_Excluded(t *testing.T) {
	result := ApplyRoute("anything", RouteExcluded, "bash")
	if !strings.Contains(result, "excluded") {
		t.Error("RouteExcluded should contain exclusion notice")
	}
}

func TestRouteContent_AggressiveSearchThreshold(t *testing.T) {
	// 400 chars: above aggressive threshold (300) but below normal (500).
	content := strings.Repeat("x", 400)

	normal := RouteContent("grep_search", content, false, nil, false)
	aggressive := RouteContent("grep_search", content, false, nil, true)

	if normal != RouteFull {
		t.Errorf("normal routing for 400-char search should be RouteFull, got %s", normal)
	}
	if aggressive != RoutePartial {
		t.Errorf("aggressive routing for 400-char search should be RoutePartial, got %s", aggressive)
	}
}

func TestRouteContent_AggressiveBashSummary(t *testing.T) {
	// 800 chars: above aggressive summary threshold (500) but below normal (1000).
	content := strings.Repeat("x", 800)

	normal := RouteContent("bash", content, false, nil, false)
	aggressive := RouteContent("bash", content, false, nil, true)

	if normal != RoutePartial {
		t.Errorf("normal routing for 800-char bash should be RoutePartial, got %s", normal)
	}
	if aggressive != RouteSummary {
		t.Errorf("aggressive routing for 800-char bash should be RouteSummary, got %s", aggressive)
	}
}

func TestPartialContent_ShortEnoughUnchanged(t *testing.T) {
	content := "short"
	result := partialContent(content, 400, 200)
	if result != content {
		t.Error("content shorter than head+tail should be unchanged")
	}
}
