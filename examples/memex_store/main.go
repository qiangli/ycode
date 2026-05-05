// Command memex_store is a smoke test that exercises the public surface of
// pkg/memex/store from outside the ycode tree. It must build and run without
// importing any internal/ packages.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	store "github.com/qiangli/ycode/pkg/memex/store"
	"github.com/qiangli/ycode/pkg/memex/store/kv"
	"github.com/qiangli/ycode/pkg/memex/store/sqlite"
)

func main() {
	dir, err := os.MkdirTemp("", "memex-store-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)

	// KV round-trip.
	k, err := kv.Open(filepath.Join(dir, "kv"))
	if err != nil {
		fail(err)
	}
	defer k.Close()

	if err := k.Put("greetings", "hello", []byte("world")); err != nil {
		fail(err)
	}
	v, err := k.Get("greetings", "hello")
	if err != nil {
		fail(err)
	}
	if string(v) != "world" {
		fail(fmt.Errorf("kv mismatch: %q", v))
	}

	// SQL round-trip with migrations.
	sqlDir := filepath.Join(dir, "sql")
	if err := os.MkdirAll(sqlDir, 0o755); err != nil {
		fail(err)
	}
	s, err := sqlite.Open(sqlDir)
	if err != nil {
		fail(err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		fail(err)
	}
	if _, err := s.Exec(ctx,
		`INSERT INTO sessions (id, title, model) VALUES (?, ?, ?)`,
		"smoke-1", "smoke session", "test",
	); err != nil {
		fail(err)
	}
	var title string
	if err := s.QueryRow(ctx, `SELECT title FROM sessions WHERE id = ?`, "smoke-1").Scan(&title); err != nil {
		fail(err)
	}
	if title != "smoke session" {
		fail(fmt.Errorf("sql mismatch: %q", title))
	}

	// Compile-time interface conformance check.
	var _ store.KVStore = k
	var _ store.SQLStore = s

	fmt.Println("pkg/memex/store: KV + SQL round-trip OK")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "memex-store smoke:", err)
	os.Exit(1)
}
