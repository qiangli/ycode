package tools

import (
	"testing"

	"github.com/qiangli/ycode/internal/runtime/sysinfo"
)

func TestFilterBySystemContext_NoInternet(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	sys := &sysinfo.SystemContext{
		HasInternet:      false,
		CanRunContainers: true,
		HasGit:           true,
	}
	FilterBySystemContext(r, sys)

	// WebFetch and WebSearch should be disabled.
	for _, name := range []string{"WebFetch", "WebSearch"} {
		spec, ok := r.Get(name)
		if !ok {
			t.Errorf("%s not found", name)
			continue
		}
		if !spec.Disabled {
			t.Errorf("%s should be disabled when HasInternet=false", name)
		}
	}

	// Other tools should NOT be disabled.
	spec, _ := r.Get("bash")
	if spec.Disabled {
		t.Error("bash should not be disabled")
	}
}

func TestFilterBySystemContext_NoContainers(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	sys := &sysinfo.SystemContext{
		HasInternet:      true,
		CanRunContainers: false,
		HasGit:           true,
		IsContainer:      true,
	}
	FilterBySystemContext(r, sys)

	// Browser tools should be disabled.
	browserTools := []string{
		"browser_navigate", "browser_click", "browser_type",
		"browser_scroll", "browser_screenshot", "browser_extract",
		"browser_back", "browser_tabs",
	}
	for _, name := range browserTools {
		spec, ok := r.Get(name)
		if !ok {
			// Browser tools may be registered inline, not in builtinSpecs.
			continue
		}
		if !spec.Disabled {
			t.Errorf("%s should be disabled when CanRunContainers=false", name)
		}
	}

	// WebFetch should still be enabled.
	spec, _ := r.Get("WebFetch")
	if spec.Disabled {
		t.Error("WebFetch should not be disabled when only containers are unavailable")
	}
}

func TestFilterBySystemContext_NoGit(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	sys := &sysinfo.SystemContext{
		HasInternet:      true,
		CanRunContainers: true,
		HasGit:           false,
	}
	FilterBySystemContext(r, sys)

	gitTools := []string{
		"git_status", "git_log", "git_commit", "git_branch",
		"git_stash", "git_add", "git_reset", "git_show", "git_grep",
		"view_diff",
		"GitServerRepoList", "GitServerRepoCreate",
	}
	for _, name := range gitTools {
		spec, ok := r.Get(name)
		if !ok {
			continue
		}
		if !spec.Disabled {
			t.Errorf("%s should be disabled when HasGit=false", name)
		}
	}
}

func TestFilterBySystemContext_CI(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	sys := &sysinfo.SystemContext{
		HasInternet:      true,
		CanRunContainers: true,
		HasGit:           true,
		IsCI:             true,
	}
	FilterBySystemContext(r, sys)

	// Interactive tools should be disabled in CI.
	for _, name := range []string{"AskUserQuestion", "SendUserMessage"} {
		spec, ok := r.Get(name)
		if !ok {
			t.Errorf("%s not found", name)
			continue
		}
		if !spec.Disabled {
			t.Errorf("%s should be disabled in CI", name)
		}
	}
}

func TestFilterBySystemContext_NilSafe(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	// Should not panic with nil SystemContext.
	FilterBySystemContext(r, nil)
}

func TestFilterBySystemContext_FullCapabilities(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	sys := &sysinfo.SystemContext{
		HasInternet:      true,
		CanRunContainers: true,
		HasGit:           true,
		IsCI:             false,
	}
	FilterBySystemContext(r, sys)

	// Nothing should be disabled.
	for _, spec := range r.All() {
		if spec.Disabled {
			t.Errorf("%s should not be disabled with full capabilities", spec.Name)
		}
	}
}

func TestDisabledToolsExcludedFromLists(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)

	// Count before disabling.
	beforeAlways := len(r.AlwaysAvailable())
	beforeDeferred := len(r.Deferred())

	// Disable a few tools.
	sys := &sysinfo.SystemContext{
		HasInternet:      false,
		CanRunContainers: true,
		HasGit:           true,
	}
	FilterBySystemContext(r, sys)

	afterAlways := len(r.AlwaysAvailable())
	afterDeferred := len(r.Deferred())

	// WebFetch and WebSearch are deferred, so always-available count should be same.
	if afterAlways != beforeAlways {
		t.Errorf("always-available count changed: %d → %d (web tools are deferred)", beforeAlways, afterAlways)
	}

	// Deferred count should decrease by 2 (WebFetch, WebSearch).
	if afterDeferred != beforeDeferred-2 {
		t.Errorf("expected deferred count to decrease by 2, got %d → %d", beforeDeferred, afterDeferred)
	}
}
