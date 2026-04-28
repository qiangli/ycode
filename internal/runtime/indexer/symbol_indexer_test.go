package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGoSymbols(t *testing.T) {
	dir := t.TempDir()
	src := `package example

type Server struct {
	port int
}

type Handler interface {
	Handle()
}

func NewServer(port int) *Server {
	return &Server{port: port}
}

func (s *Server) Start() error {
	return nil
}

const Version = "1.0"
var debug = false
`
	path := filepath.Join(dir, "example.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := extractGoSymbols(path, "example.go")

	// Should find: Server (type), Handler (interface), NewServer (func), Start (method), Version (const), debug (var)
	if len(symbols) < 6 {
		t.Fatalf("expected at least 6 symbols, got %d", len(symbols))
	}

	// Verify specific symbols.
	found := make(map[string]SymbolDoc)
	for _, s := range symbols {
		found[s.Name] = s
	}

	if s, ok := found["Server"]; !ok {
		t.Error("missing Server type")
	} else if s.Kind != "type" || !s.Exported {
		t.Errorf("Server: kind=%s exported=%v, want type/true", s.Kind, s.Exported)
	}

	if s, ok := found["Handler"]; !ok {
		t.Error("missing Handler interface")
	} else if s.Kind != "interface" {
		t.Errorf("Handler: kind=%s, want interface", s.Kind)
	}

	if s, ok := found["NewServer"]; !ok {
		t.Error("missing NewServer func")
	} else if s.Kind != "func" {
		t.Errorf("NewServer: kind=%s, want func", s.Kind)
	}

	if s, ok := found["Start"]; !ok {
		t.Error("missing Start method")
	} else if s.Kind != "method" {
		t.Errorf("Start: kind=%s, want method", s.Kind)
	}

	if s, ok := found["Version"]; !ok {
		t.Error("missing Version const")
	} else if s.Kind != "const" {
		t.Errorf("Version: kind=%s, want const", s.Kind)
	}

	if s, ok := found["debug"]; !ok {
		t.Error("missing debug var")
	} else if s.Kind != "var" || s.Exported {
		t.Errorf("debug: kind=%s exported=%v, want var/false", s.Kind, s.Exported)
	}
}

func TestExtractRegexSymbols_Python(t *testing.T) {
	dir := t.TempDir()
	src := `class MyClass:
    def __init__(self):
        pass

    def process(self):
        pass

async def fetch_data():
    pass

def _private_helper():
    pass
`
	path := filepath.Join(dir, "test.py")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := extractRegexSymbols(path, "test.py", "py")

	if len(symbols) < 4 {
		t.Fatalf("expected at least 4 symbols, got %d", len(symbols))
	}

	found := make(map[string]SymbolDoc)
	for _, s := range symbols {
		found[s.Name] = s
	}

	if s, ok := found["MyClass"]; !ok {
		t.Error("missing MyClass")
	} else if s.Kind != "class" {
		t.Errorf("MyClass: kind=%s, want class", s.Kind)
	}

	if s, ok := found["fetch_data"]; !ok {
		t.Error("missing fetch_data")
	} else if s.Kind != "func" {
		t.Errorf("fetch_data: kind=%s, want func", s.Kind)
	}

	if s, ok := found["_private_helper"]; !ok {
		t.Error("missing _private_helper")
	} else if s.Exported {
		t.Error("_private_helper should not be exported")
	}
}

func TestExtractRegexSymbols_TypeScript(t *testing.T) {
	dir := t.TempDir()
	src := `export function handleRequest(req: Request): Response {
}

export class APIServer {
}

export interface Config {
}

export type Options = {
}

const helper = function() {}
`
	path := filepath.Join(dir, "test.ts")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := extractRegexSymbols(path, "test.ts", "ts")

	if len(symbols) < 4 {
		t.Fatalf("expected at least 4 symbols, got %d: %+v", len(symbols), symbols)
	}

	found := make(map[string]SymbolDoc)
	for _, s := range symbols {
		found[s.Name] = s
	}

	if _, ok := found["handleRequest"]; !ok {
		t.Error("missing handleRequest")
	}
	if _, ok := found["APIServer"]; !ok {
		t.Error("missing APIServer")
	}
	if _, ok := found["Config"]; !ok {
		t.Error("missing Config")
	}
	if _, ok := found["Options"]; !ok {
		t.Error("missing Options")
	}
}

func TestExtractRegexSymbols_Rust(t *testing.T) {
	dir := t.TempDir()
	src := `pub fn serve(port: u16) {}
fn helper() {}
pub struct Server {}
pub enum Status { Ok, Err }
pub trait Handler {}
impl Server {}
`
	path := filepath.Join(dir, "test.rs")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	symbols := extractRegexSymbols(path, "test.rs", "rs")

	if len(symbols) < 5 {
		t.Fatalf("expected at least 5 symbols, got %d", len(symbols))
	}

	found := make(map[string]SymbolDoc)
	for _, s := range symbols {
		found[s.Name] = s
	}

	if s, ok := found["serve"]; !ok {
		t.Error("missing serve")
	} else if s.Kind != "func" {
		t.Errorf("serve: kind=%s, want func", s.Kind)
	}

	if s, ok := found["Handler"]; !ok {
		t.Error("missing Handler")
	} else if s.Kind != "interface" {
		t.Errorf("Handler: kind=%s, want interface", s.Kind)
	}
}
