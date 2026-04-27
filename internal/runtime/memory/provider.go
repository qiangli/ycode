package memory

import (
	"context"
	"strings"
)

// MemoryProvider is the interface for pluggable memory backends.
// The file-based Store is the default implementation. External providers
// (e.g., Mem0, Honcho) can implement this interface and register via plugins.
type MemoryProvider interface {
	// Core operations
	Save(mem *Memory) error
	Load(path string) (*Memory, error)
	List() ([]*Memory, error)
	Delete(path string) error
	Dir() string

	// Search returns memories matching the query, up to maxResults.
	Search(ctx context.Context, query string, maxResults int) ([]*Memory, error)

	// Lifecycle hooks — called at key moments in the conversation.
	// Default implementations should be no-ops.

	// OnTurnStart is called at the beginning of each conversation turn.
	OnTurnStart(ctx context.Context, remainingTokens int) error

	// OnPreCompress is called before context compression occurs.
	OnPreCompress(ctx context.Context) error

	// OnMemoryWrite is called when a memory is saved through any mechanism.
	OnMemoryWrite(ctx context.Context, mem *Memory) error

	// OnDelegation is called when a subagent completes its work.
	OnDelegation(ctx context.Context, agentType string, result string) error

	// OnSessionEnd is called when the conversation session ends.
	OnSessionEnd(ctx context.Context) error
}

// FileProvider wraps the existing Store to implement MemoryProvider.
// This is the default provider used when no external provider is configured.
type FileProvider struct {
	store *Store
}

// NewFileProvider creates a MemoryProvider backed by the filesystem.
func NewFileProvider(dir string) (*FileProvider, error) {
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	return &FileProvider{store: store}, nil
}

func (fp *FileProvider) Save(mem *Memory) error            { return fp.store.Save(mem) }
func (fp *FileProvider) Load(path string) (*Memory, error) { return fp.store.Load(path) }
func (fp *FileProvider) List() ([]*Memory, error)          { return fp.store.List() }
func (fp *FileProvider) Delete(path string) error          { return fp.store.Delete(path) }
func (fp *FileProvider) Dir() string                       { return fp.store.Dir() }

// Search performs a basic keyword search across all memories.
func (fp *FileProvider) Search(ctx context.Context, query string, maxResults int) ([]*Memory, error) {
	all, err := fp.store.List()
	if err != nil {
		return nil, err
	}
	// Simple keyword matching fallback.
	query = strings.ToLower(query)
	var results []*Memory
	for _, mem := range all {
		if strings.Contains(strings.ToLower(mem.Name), query) ||
			strings.Contains(strings.ToLower(mem.Content), query) ||
			strings.Contains(strings.ToLower(mem.Description), query) {
			results = append(results, mem)
			if len(results) >= maxResults {
				break
			}
		}
	}
	return results, nil
}

// Lifecycle hooks — no-ops for file-based provider.
func (fp *FileProvider) OnTurnStart(ctx context.Context, remainingTokens int) error { return nil }
func (fp *FileProvider) OnPreCompress(ctx context.Context) error                    { return nil }
func (fp *FileProvider) OnMemoryWrite(ctx context.Context, mem *Memory) error       { return nil }
func (fp *FileProvider) OnDelegation(ctx context.Context, agentType string, result string) error {
	return nil
}
func (fp *FileProvider) OnSessionEnd(ctx context.Context) error { return nil }
