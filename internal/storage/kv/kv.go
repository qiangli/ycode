// Package kv provides a bbolt-backed key-value store with bucket-based namespacing.
//
// bbolt is a pure Go B+ tree key-value store with full ACID transactions,
// lock-free MVCC, and single-file storage. It excels at read-heavy workloads
// like config lookups, permission checks, and metadata caching.
package kv

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qiangli/ycode/internal/storage"

	bolt "go.etcd.io/bbolt"
)

// Store implements storage.KVStore backed by bbolt.
type Store struct {
	db *bolt.DB
}

// Open creates or opens a bbolt database at the given directory.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create kv dir: %w", err)
	}

	dbPath := filepath.Join(dir, "ycode.kv")
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{
		Timeout:      200 * time.Millisecond, // Fail fast if another process holds the lock.
		NoGrowSync:   false,
		FreelistType: bolt.FreelistMapType,
	})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}

	return &Store{db: db}, nil
}

// Get retrieves a value by bucket and key.
func (s *Store) Get(bucket, key string) ([]byte, error) {
	var value []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v != nil {
			value = make([]byte, len(v))
			copy(value, v)
		}
		return nil
	})
	return value, err
}

// Put stores a value in a bucket.
func (s *Store) Put(bucket, key string, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return fmt.Errorf("create bucket %q: %w", bucket, err)
		}
		return b.Put([]byte(key), value)
	})
}

// Delete removes a key from a bucket.
func (s *Store) Delete(bucket, key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

// List returns all keys in a bucket.
func (s *Store) List(bucket string) ([]string, error) {
	var keys []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			keys = append(keys, string(k))
			return nil
		})
	})
	return keys, err
}

// ForEach iterates over all key-value pairs in a bucket.
func (s *Store) ForEach(bucket string, fn func(key string, value []byte) error) error {
	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			return fn(string(k), v)
		})
	})
}

// Close closes the bbolt database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *bolt.DB for advanced use cases.
func (s *Store) DB() *bolt.DB {
	return s.db
}

// compile-time interface check.
var _ storage.KVStore = (*Store)(nil)
