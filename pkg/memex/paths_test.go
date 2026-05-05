package memex

import (
	"testing"

	"github.com/qiangli/ycode/pkg/memex/memory"
)

func TestMemoryPath_RoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		mem       memory.Memory
		want      string
		scope     memory.Scope
		scopePath string
		mType     memory.Type
	}{
		{
			name:  "global feedback",
			mem:   memory.Memory{Name: "prefer-table", Scope: memory.ScopeGlobal, Type: memory.TypeFeedback},
			want:  "/memory/global/feedback/prefer-table.md",
			scope: memory.ScopeGlobal, mType: memory.TypeFeedback,
		},
		{
			name:  "project-scoped with scopePath",
			mem:   memory.Memory{Name: "auth-flow", Scope: memory.ScopeTeam, ScopePath: "backend", Type: memory.TypeReference},
			want:  "/memory/team/backend/reference/auth-flow.md",
			scope: memory.ScopeTeam, scopePath: "backend", mType: memory.TypeReference,
		},
		{
			name:  "default project scope",
			mem:   memory.Memory{Name: "todo", Type: memory.TypeProject},
			want:  "/memory/project/project/todo.md",
			scope: memory.ScopeProject, mType: memory.TypeProject,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MemoryPath(tc.mem)
			if got != tc.want {
				t.Errorf("MemoryPath = %q, want %q", got, tc.want)
			}
			scope, scopePath, mType, name, err := ParseMemoryPath(got)
			if err != nil {
				t.Fatalf("ParseMemoryPath(%q): %v", got, err)
			}
			if scope != tc.scope {
				t.Errorf("scope = %q, want %q", scope, tc.scope)
			}
			if scopePath != tc.scopePath {
				t.Errorf("scopePath = %q, want %q", scopePath, tc.scopePath)
			}
			if mType != tc.mType {
				t.Errorf("type = %q, want %q", mType, tc.mType)
			}
			if name != tc.mem.Name {
				t.Errorf("name = %q, want %q", name, tc.mem.Name)
			}
		})
	}
}

func TestParseMemoryPath_Errors(t *testing.T) {
	cases := []string{
		"/notes/x.md",
		"/memos/42",
		"/memory",
		"",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if _, _, _, _, err := ParseMemoryPath(p); err == nil {
				t.Errorf("ParseMemoryPath(%q) succeeded, want error", p)
			}
		})
	}
}

func TestMemoPath(t *testing.T) {
	if got := MemoPath("42"); got != "/memos/42" {
		t.Errorf("MemoPath = %q, want /memos/42", got)
	}
	if got := MemoTagPath("auth"); got != "/memos/tag/auth/" {
		t.Errorf("MemoTagPath = %q, want /memos/tag/auth/", got)
	}
	id, ok := ParseMemoPath("/memos/42")
	if !ok || id != "42" {
		t.Errorf("ParseMemoPath = %q, %v; want 42, true", id, ok)
	}
	if _, ok := ParseMemoPath("/memos/tag/foo/"); ok {
		t.Error("ParseMemoPath should reject tag dir as memo id")
	}
	if _, ok := ParseMemoPath("/memory/global/x.md"); ok {
		t.Error("ParseMemoPath should reject memory paths")
	}
}
