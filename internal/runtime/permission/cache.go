package permission

import (
	"encoding/json"
	"log/slog"

	"github.com/qiangli/ycode/internal/storage"
)

const (
	kvBucket      = "permission_rules"
	kvKeyPolicy   = "active_policy"
	kvKeyApproved = "approved_tools"
)

// Cache persists permission policies and approval history in a KV store.
// This allows permission decisions to survive process restarts, so users
// don't have to re-approve the same tools every session.
type Cache struct {
	kv storage.KVStore
}

// NewCache creates a permission cache backed by the given KV store.
func NewCache(kv storage.KVStore) *Cache {
	return &Cache{kv: kv}
}

// StorePolicy persists the active permission policy.
func (c *Cache) StorePolicy(policy *Policy) {
	data, err := json.Marshal(policy)
	if err != nil {
		slog.Debug("permission cache: marshal policy", "error", err)
		return
	}
	if err := c.kv.Put(kvBucket, kvKeyPolicy, data); err != nil {
		slog.Debug("permission cache: store policy", "error", err)
	}
}

// LoadPolicy retrieves the cached permission policy.
// Returns nil if no policy is cached.
func (c *Cache) LoadPolicy() *Policy {
	data, err := c.kv.Get(kvBucket, kvKeyPolicy)
	if err != nil || data == nil {
		return nil
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil
	}
	return &policy
}

// RecordApproval records that a user approved a specific tool.
// This can be used to auto-approve tools the user has previously allowed.
func (c *Cache) RecordApproval(toolName string) {
	approved := c.loadApproved()
	approved[toolName] = true
	c.saveApproved(approved)
}

// IsApproved checks whether a tool was previously approved by the user.
func (c *Cache) IsApproved(toolName string) bool {
	approved := c.loadApproved()
	return approved[toolName]
}

// ClearApprovals removes all recorded approvals.
func (c *Cache) ClearApprovals() {
	if err := c.kv.Delete(kvBucket, kvKeyApproved); err != nil {
		slog.Debug("permission cache: clear approvals", "error", err)
	}
}

func (c *Cache) loadApproved() map[string]bool {
	data, err := c.kv.Get(kvBucket, kvKeyApproved)
	if err != nil || data == nil {
		return make(map[string]bool)
	}
	var approved map[string]bool
	if err := json.Unmarshal(data, &approved); err != nil {
		return make(map[string]bool)
	}
	return approved
}

func (c *Cache) saveApproved(approved map[string]bool) {
	data, err := json.Marshal(approved)
	if err != nil {
		return
	}
	if err := c.kv.Put(kvBucket, kvKeyApproved, data); err != nil {
		slog.Debug("permission cache: save approvals", "error", err)
	}
}
