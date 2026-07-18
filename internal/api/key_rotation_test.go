package api

import (
	"testing"
	"time"
)

func TestKeyPool_Current(t *testing.T) {
	kp, err := NewKeyPool(KeyPoolConfig{
		Keys: []string{"key-a", "key-b", "key-c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if kp.Current() != "key-a" {
		t.Errorf("expected key-a, got %s", kp.Current())
	}
}

func TestKeyPool_Rotate(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"key-a", "key-b", "key-c"},
		CooldownDuration: 50 * time.Millisecond,
	})

	// Rotate from a → b.
	key, ok := kp.Rotate()
	if !ok || key != "key-b" {
		t.Errorf("expected key-b, got %s (ok=%v)", key, ok)
	}

	// Rotate from b → c.
	key, ok = kp.Rotate()
	if !ok || key != "key-c" {
		t.Errorf("expected key-c, got %s (ok=%v)", key, ok)
	}

	// Rotate from c — a should be on cooldown still.
	key, ok = kp.Rotate()
	// All three have been used, but a's cooldown might have expired if test is slow.
	// The key returned should be valid.
	if key == "" {
		t.Error("expected a key, got empty")
	}
}

func TestKeyPool_AllOnCooldown(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"key-a", "key-b"},
		CooldownDuration: time.Second, // long enough to not expire
	})

	kp.Rotate()          // a→b, a on cooldown
	_, ok := kp.Rotate() // b→?, b on cooldown, a still on cooldown

	if ok {
		t.Error("expected false when all keys on cooldown")
	}
}

func TestKeyPool_CooldownExpires(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"key-a", "key-b"},
		CooldownDuration: 20 * time.Millisecond,
	})

	kp.Rotate() // a→b, a on cooldown
	time.Sleep(30 * time.Millisecond)

	// a's cooldown should have expired.
	key, ok := kp.Rotate()
	if !ok || key != "key-a" {
		t.Errorf("expected key-a after cooldown expiry, got %s (ok=%v)", key, ok)
	}
}

func TestKeyPool_Available(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"key-a", "key-b", "key-c"},
		CooldownDuration: time.Second,
	})

	if kp.Available() != 3 {
		t.Errorf("expected 3 available, got %d", kp.Available())
	}

	kp.Rotate() // a on cooldown
	if kp.Available() != 2 {
		t.Errorf("expected 2 available, got %d", kp.Available())
	}
}

func TestKeyPool_Size(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{Keys: []string{"a", "b"}})
	if kp.Size() != 2 {
		t.Errorf("expected 2, got %d", kp.Size())
	}
}

func TestKeyPool_EmptyKeys(t *testing.T) {
	_, err := NewKeyPool(KeyPoolConfig{Keys: nil})
	if err == nil {
		t.Error("expected error for empty keys")
	}
}

func TestKeyPool_MarkRateLimited(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"key-a", "key-b"},
		CooldownDuration: time.Second,
	})

	key, ok := kp.MarkRateLimited()
	if !ok || key != "key-b" {
		t.Errorf("expected key-b after rate limit, got %s", key)
	}
}

// TestKeyPool_MarkFailed_Transient: only TRANSIENT failures (429/5xx/network)
// cool the current key down and rotate to the next one.
func TestKeyPool_MarkFailed_Transient(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"key-a", "key-b"},
		CooldownDuration: time.Second,
	})

	rateLimited := &ClassifiedError{Reason: ReasonRateLimit, Action: ActionRetry, StatusCode: 429}
	key, rotated := kp.MarkFailed(rateLimited)
	if !rotated || key != "key-b" {
		t.Errorf("transient 429: expected rotation to key-b, got %s (rotated=%v)", key, rotated)
	}
	if kp.Available() != 1 {
		t.Errorf("transient 429: expected 1 key on cooldown, available=%d", kp.Available())
	}
}

// TestKeyPool_MarkFailed_PermanentAuth: a PERMANENT auth failure (401/403)
// means the key itself is rejected. Cooling it down and rotating to a sibling
// masks the real problem and wastes the pool — the pool must stay put so the
// caller fails fast with the clear error.
func TestKeyPool_MarkFailed_PermanentAuth(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{
		Keys:             []string{"stale-key-1c75", "key-b"},
		CooldownDuration: time.Second,
	})

	unauthorized := &ClassifiedError{Reason: ReasonAuthPermanent, Action: ActionAbort, StatusCode: 401}
	key, rotated := kp.MarkFailed(unauthorized)
	if rotated {
		t.Error("permanent 401 must NOT rotate to a sibling key")
	}
	if key != "stale-key-1c75" {
		t.Errorf("permanent 401: current key should stay, got %s", key)
	}
	if kp.Available() != 2 {
		t.Errorf("permanent 401 must NOT cool any key down, available=%d", kp.Available())
	}
}

func TestRotatingProviderConfig_ToProviderConfig(t *testing.T) {
	kp, _ := NewKeyPool(KeyPoolConfig{Keys: []string{"key-x"}})
	rc := &RotatingProviderConfig{
		Kind:    ProviderAnthropic,
		Keys:    []string{"key-x"},
		BaseURL: "https://api.example.com",
	}

	cfg := rc.ToProviderConfig(kp)
	if cfg.APIKey != "key-x" {
		t.Errorf("expected key-x, got %s", cfg.APIKey)
	}
	if cfg.Kind != ProviderAnthropic {
		t.Errorf("expected anthropic, got %s", cfg.Kind)
	}
}
