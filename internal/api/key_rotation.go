package api

import (
	"fmt"
	"sync"
	"time"
)

// KeyPool manages a pool of API keys for a single provider,
// rotating to the next key when the current one hits a rate limit.
type KeyPool struct {
	mu               sync.Mutex
	keys             []string
	current          int
	cooldowns        map[int]time.Time
	cooldownDuration time.Duration
}

// KeyPoolConfig configures a key pool.
type KeyPoolConfig struct {
	Keys             []string      // API keys to rotate through
	CooldownDuration time.Duration // how long to wait before reusing a rate-limited key (default 60s)
}

// NewKeyPool creates a key pool from the given config.
func NewKeyPool(cfg KeyPoolConfig) (*KeyPool, error) {
	if len(cfg.Keys) == 0 {
		return nil, fmt.Errorf("key pool requires at least one key")
	}
	cd := cfg.CooldownDuration
	if cd <= 0 {
		cd = 60 * time.Second
	}
	return &KeyPool{
		keys:             cfg.Keys,
		cooldowns:        make(map[int]time.Time),
		cooldownDuration: cd,
	}, nil
}

// Current returns the currently active API key.
func (kp *KeyPool) Current() string {
	kp.mu.Lock()
	defer kp.mu.Unlock()
	return kp.keys[kp.current]
}

// Rotate moves to the next available key, placing the current key on cooldown.
// Returns the new active key and true if rotation succeeded,
// or the current key and false if all keys are on cooldown.
func (kp *KeyPool) Rotate() (string, bool) {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	// Put current on cooldown.
	kp.cooldowns[kp.current] = time.Now().Add(kp.cooldownDuration)

	// Find next available key.
	for i := 1; i <= len(kp.keys); i++ {
		idx := (kp.current + i) % len(kp.keys)
		if !kp.isOnCooldown(idx) {
			kp.current = idx
			return kp.keys[idx], true
		}
	}

	// All keys on cooldown — return current (least recently rate-limited).
	return kp.keys[kp.current], false
}

// MarkRateLimited puts the current key on cooldown and rotates.
func (kp *KeyPool) MarkRateLimited() (string, bool) {
	return kp.Rotate()
}

// Available returns the number of keys not on cooldown.
func (kp *KeyPool) Available() int {
	kp.mu.Lock()
	defer kp.mu.Unlock()
	count := 0
	for i := range kp.keys {
		if !kp.isOnCooldown(i) {
			count++
		}
	}
	return count
}

// Size returns the total number of keys in the pool.
func (kp *KeyPool) Size() int {
	return len(kp.keys)
}

func (kp *KeyPool) isOnCooldown(idx int) bool {
	expiry, ok := kp.cooldowns[idx]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(kp.cooldowns, idx)
		return false
	}
	return true
}

// RotatingProviderConfig extends ProviderConfig with multiple keys.
type RotatingProviderConfig struct {
	Kind             ProviderKind
	DisplayName      string
	Keys             []string // multiple API keys
	BaseURL          string
	CooldownDuration time.Duration // default 60s
}

// ToProviderConfig creates a ProviderConfig using the current key from the pool.
func (rc *RotatingProviderConfig) ToProviderConfig(pool *KeyPool) ProviderConfig {
	return ProviderConfig{
		Kind:        rc.Kind,
		DisplayName: rc.DisplayName,
		APIKey:      pool.Current(),
		BaseURL:     rc.BaseURL,
	}
}
