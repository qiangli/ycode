package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/qiangli/ycode/internal/storage"
)

const (
	kvBucket         = "config_cache"
	kvKeyConfig      = "merged_config"
	kvKeyFingerprint = "source_fingerprint"
)

// Cache wraps a KVStore to persist the merged config.
// It stores the serialized Config and a fingerprint of the source files
// so stale detection works across process restarts.
type Cache struct {
	kv          storage.KVStore
	sourcePaths []string
}

// NewCache creates a config cache backed by the given KV store.
// sourcePaths are the config file paths used to compute staleness fingerprints.
func NewCache(kv storage.KVStore, sourcePaths []string) *Cache {
	return &Cache{kv: kv, sourcePaths: sourcePaths}
}

// Store persists the merged config and the current source fingerprint.
func (c *Cache) Store(cfg *Config) {
	data, err := json.Marshal(cfg)
	if err != nil {
		slog.Debug("config cache: marshal", "error", err)
		return
	}

	if err := c.kv.Put(kvBucket, kvKeyConfig, data); err != nil {
		slog.Debug("config cache: put config", "error", err)
		return
	}

	fp := c.fingerprint()
	if err := c.kv.Put(kvBucket, kvKeyFingerprint, []byte(fp)); err != nil {
		slog.Debug("config cache: put fingerprint", "error", err)
	}
}

// Load retrieves the cached config if the source files haven't changed.
// Returns nil if the cache is stale or empty.
func (c *Cache) Load() *Config {
	// Check fingerprint first.
	storedFP, err := c.kv.Get(kvBucket, kvKeyFingerprint)
	if err != nil || storedFP == nil {
		return nil
	}

	currentFP := c.fingerprint()
	if string(storedFP) != currentFP {
		return nil // source files changed
	}

	data, err := c.kv.Get(kvBucket, kvKeyConfig)
	if err != nil || data == nil {
		return nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// fingerprint computes a hash over the source config file contents and mtimes.
func (c *Cache) fingerprint() string {
	h := sha256.New()
	for _, path := range c.sourcePaths {
		info, err := os.Stat(path)
		if err != nil {
			h.Write([]byte("missing:" + path))
			continue
		}
		h.Write([]byte(path))
		h.Write([]byte(info.ModTime().String()))
		// Include file size as a fast-change indicator.
		data := make([]byte, 8)
		size := info.Size()
		for i := range data {
			data[i] = byte(size >> (i * 8))
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
