package kv

import (
	"os"
	"testing"
)

func TestStore(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	t.Run("PutGet", func(t *testing.T) {
		if err := s.Put("test", "key1", []byte("value1")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		val, err := s.Get("test", "key1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(val) != "value1" {
			t.Errorf("Get = %q, want %q", val, "value1")
		}
	})

	t.Run("GetMissing", func(t *testing.T) {
		val, err := s.Get("test", "nonexistent")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if val != nil {
			t.Errorf("Get missing key = %q, want nil", val)
		}
	})

	t.Run("GetMissingBucket", func(t *testing.T) {
		val, err := s.Get("no_such_bucket", "key")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if val != nil {
			t.Errorf("Get missing bucket = %q, want nil", val)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := s.Put("test", "delme", []byte("gone")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := s.Delete("test", "delme"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		val, err := s.Get("test", "delme")
		if err != nil {
			t.Fatalf("Get after Delete: %v", err)
		}
		if val != nil {
			t.Errorf("Get after Delete = %q, want nil", val)
		}
	})

	t.Run("List", func(t *testing.T) {
		if err := s.Put("listbucket", "a", []byte("1")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := s.Put("listbucket", "b", []byte("2")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		keys, err := s.List("listbucket")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(keys) != 2 {
			t.Errorf("List len = %d, want 2", len(keys))
		}
	})

	t.Run("ForEach", func(t *testing.T) {
		count := 0
		err := s.ForEach("listbucket", func(key string, value []byte) error {
			count++
			return nil
		})
		if err != nil {
			t.Fatalf("ForEach: %v", err)
		}
		if count != 2 {
			t.Errorf("ForEach count = %d, want 2", count)
		}
	})
}

func TestStoreFileCreated(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	// Verify the database file was created.
	if _, err := os.Stat(dir + "/ycode.kv"); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}
