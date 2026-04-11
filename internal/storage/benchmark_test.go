package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/qiangli/ycode/internal/storage"
	"github.com/qiangli/ycode/internal/storage/kv"
	"github.com/qiangli/ycode/internal/storage/search"
	"github.com/qiangli/ycode/internal/storage/sqlite"
)

// BenchmarkKVPut measures bbolt write throughput.
func BenchmarkKVPut(b *testing.B) {
	dir := b.TempDir()
	s, err := kv.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	i := 0
	for b.Loop() {
		key := fmt.Sprintf("key-%d", i)
		if err := s.Put("bench", key, []byte("value")); err != nil {
			b.Fatalf("Put: %v", err)
		}
		i++
	}
}

// BenchmarkKVGet measures bbolt read throughput.
func BenchmarkKVGet(b *testing.B) {
	dir := b.TempDir()
	s, err := kv.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Seed data.
	for i := range 1000 {
		key := fmt.Sprintf("key-%d", i)
		if err := s.Put("bench", key, []byte("value")); err != nil {
			b.Fatalf("Put: %v", err)
		}
	}

	i := 0
	b.ResetTimer()
	for b.Loop() {
		key := fmt.Sprintf("key-%d", i%1000)
		if _, err := s.Get("bench", key); err != nil {
			b.Fatalf("Get: %v", err)
		}
		i++
	}
}

// BenchmarkSQLiteInsert measures SQLite insert throughput.
func BenchmarkSQLiteInsert(b *testing.B) {
	dir := b.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		b.Fatalf("Migrate: %v", err)
	}

	i := 0
	b.ResetTimer()
	for b.Loop() {
		id := fmt.Sprintf("sess-%d", i)
		if _, err := s.Exec(ctx,
			`INSERT OR REPLACE INTO sessions (id, title, model) VALUES (?, ?, ?)`,
			id, "bench session", "test-model",
		); err != nil {
			b.Fatalf("Insert: %v", err)
		}
		i++
	}
}

// BenchmarkSQLiteQuery measures SQLite read throughput.
func BenchmarkSQLiteQuery(b *testing.B) {
	dir := b.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		b.Fatalf("Migrate: %v", err)
	}

	// Seed data.
	for i := range 1000 {
		id := fmt.Sprintf("sess-%d", i)
		if _, err := s.Exec(ctx,
			`INSERT INTO sessions (id, title, model) VALUES (?, ?, ?)`,
			id, fmt.Sprintf("session %d", i), "test-model",
		); err != nil {
			b.Fatalf("Insert: %v", err)
		}
	}

	i := 0
	b.ResetTimer()
	for b.Loop() {
		id := fmt.Sprintf("sess-%d", i%1000)
		var title string
		if err := s.QueryRow(ctx,
			`SELECT title FROM sessions WHERE id = ?`, id,
		).Scan(&title); err != nil {
			b.Fatalf("QueryRow: %v", err)
		}
		i++
	}
}

// BenchmarkBleveIndex measures Bleve indexing throughput.
func BenchmarkBleveIndex(b *testing.B) {
	dir := b.TempDir()
	s, err := search.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	i := 0
	b.ResetTimer()
	for b.Loop() {
		doc := storage.Document{
			ID:      fmt.Sprintf("doc-%d", i),
			Content: "The quick brown fox jumps over the lazy dog in a persistent storage benchmark test.",
			Metadata: map[string]string{
				"type": "bench",
				"path": fmt.Sprintf("/file-%d.go", i),
			},
		}
		if err := s.Index(ctx, "bench", doc); err != nil {
			b.Fatalf("Index: %v", err)
		}
		i++
	}
}

// BenchmarkBleveSearch measures Bleve search throughput.
func BenchmarkBleveSearch(b *testing.B) {
	dir := b.TempDir()
	s, err := search.Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Seed data.
	docs := make([]storage.Document, 500)
	for i := range docs {
		docs[i] = storage.Document{
			ID:      fmt.Sprintf("doc-%d", i),
			Content: fmt.Sprintf("Function number %d handles authentication and authorization for the API gateway service", i),
			Metadata: map[string]string{
				"type": "code",
				"path": fmt.Sprintf("/internal/handler_%d.go", i),
			},
		}
	}
	if err := s.BatchIndex(ctx, "bench", docs); err != nil {
		b.Fatalf("BatchIndex: %v", err)
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Search(ctx, "bench", "authentication gateway", 10); err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}
