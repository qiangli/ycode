//go:build integration

package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestIntegration_GoplsDefinition tests go-to-definition with a real gopls server.
func TestIntegration_GoplsDefinition(t *testing.T) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls not found in PATH")
	}
	t.Logf("using gopls at %s", goplsPath)

	// Create a minimal Go project.
	dir := t.TempDir()
	writeGoProject(t, dir)

	client := NewClient(ServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	})
	client.SetRootDir(dir)
	defer client.Close()

	// Give gopls a moment to initialize and index.
	time.Sleep(2 * time.Second)

	// Go to definition of "Add" call in main.go (line 5, col 8).
	locs, err := client.Definition(filepath.Join(dir, "main.go"), 5, 8)
	if err != nil {
		t.Fatalf("Definition failed: %v", err)
	}

	if len(locs) == 0 {
		t.Fatal("expected at least one definition location")
	}

	// Should point to lib.go where Add is defined.
	found := false
	for _, loc := range locs {
		if filepath.Base(loc.URI) == "lib.go" || filepath.Base(loc.URI) == "lib.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected definition in lib.go, got: %+v", locs)
	}
}

// TestIntegration_GoplsReferences tests find-references with a real gopls server.
func TestIntegration_GoplsReferences(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH")
	}

	dir := t.TempDir()
	writeGoProject(t, dir)

	client := NewClient(ServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	})
	client.SetRootDir(dir)
	defer client.Close()

	time.Sleep(2 * time.Second)

	// Find references to "Add" function defined in lib.go (line 2, col 5).
	refs, err := client.References(filepath.Join(dir, "lib.go"), 2, 5)
	if err != nil {
		t.Fatalf("References failed: %v", err)
	}

	// Should find at least 2 references: the definition and the usage in main.go.
	if len(refs) < 2 {
		t.Errorf("expected at least 2 references, got %d: %+v", len(refs), refs)
	}
}

// TestIntegration_GoplsSymbols tests document symbols with a real gopls server.
func TestIntegration_GoplsSymbols(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH")
	}

	dir := t.TempDir()
	writeGoProject(t, dir)

	client := NewClient(ServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	})
	client.SetRootDir(dir)
	defer client.Close()

	time.Sleep(2 * time.Second)

	syms, err := client.Symbols(filepath.Join(dir, "lib.go"))
	if err != nil {
		t.Fatalf("Symbols failed: %v", err)
	}

	if len(syms) == 0 {
		t.Fatal("expected at least one symbol")
	}

	found := false
	for _, s := range syms {
		if s.Name == "Add" && s.Kind == "Function" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Add function symbol")
		for _, s := range syms {
			t.Logf("  symbol: %s (%s)", s.Name, s.Kind)
		}
	}
}

// TestIntegration_GoplsHover tests hover with a real gopls server.
func TestIntegration_GoplsHover(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH")
	}

	dir := t.TempDir()
	writeGoProject(t, dir)

	client := NewClient(ServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	})
	client.SetRootDir(dir)
	defer client.Close()

	time.Sleep(2 * time.Second)

	hover, err := client.Hover(filepath.Join(dir, "lib.go"), 2, 5)
	if err != nil {
		t.Fatalf("Hover failed: %v", err)
	}

	if hover == nil {
		t.Fatal("expected hover result")
	}
	if hover.Contents == "" {
		t.Error("expected non-empty hover contents")
	}
	t.Logf("hover contents: %s", hover.Contents)
}

// TestIntegration_ClientRegistry tests end-to-end registry flow with gopls.
func TestIntegration_ClientRegistry(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH")
	}

	dir := t.TempDir()
	writeGoProject(t, dir)

	reg := NewClientRegistry()
	client := NewClient(ServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	})
	client.SetRootDir(dir)
	reg.Register("go", client)
	defer reg.Close()

	time.Sleep(2 * time.Second)

	// Use Execute dispatcher.
	req := &Request{
		Action:   ActionDefinition,
		FilePath: filepath.Join(dir, "main.go"),
		Line:     5,
		Col:      8,
		Language: "go",
	}

	goClient, ok := reg.Get("go")
	if !ok {
		t.Fatal("go client not found")
	}

	resp, err := Execute(goClient, req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	formatted := FormatResponse(resp)
	if formatted == "" {
		t.Error("expected non-empty formatted response")
	}
	t.Logf("formatted: %s", formatted)
}

// TestIntegration_GoplsShutdown verifies clean server shutdown.
func TestIntegration_GoplsShutdown(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found in PATH")
	}

	dir := t.TempDir()
	writeGoProject(t, dir)

	client := NewClient(ServerConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	})
	client.SetRootDir(dir)

	// Force connection by making a call.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.ensureConnected(ctx)

	// Close should not panic or hang.
	client.Close()
}

// writeGoProject creates a minimal Go project for LSP testing.
func writeGoProject(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"go.mod": "module lsptest\n\ngo 1.22\n",
		"lib.go": `package lsptest

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}

// Config holds configuration.
type Config struct {
	Name string
	Port int
}
`,
		"main.go": `package lsptest

import "fmt"

func main() {
	result := Add(1, 2)
	fmt.Println(result)
}
`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
