package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Stored timestamps remain authoritative; legacy or partially populated
// frontmatter falls back independently for each missing timestamp.
func TestStoreLoadTimestampFallback(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	persisted := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	mtime := persisted.Add(24 * time.Hour)
	for _, tc := range []struct {
		name, fields     string
		created, updated time.Time
	}{
		{"both", "created_at: " + persisted.Format(time.RFC3339) + "\nupdated_at: " + persisted.Format(time.RFC3339) + "\n", persisted, persisted},
		{"created", "created_at: " + persisted.Format(time.RFC3339) + "\n", persisted, mtime},
		{"updated", "updated_at: " + persisted.Format(time.RFC3339) + "\n", mtime, persisted},
		{"legacy", "", mtime, mtime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(store.Dir(), tc.name+".md")
			if err := os.WriteFile(path, []byte("---\nname: example\n"+tc.fields+"---\n\ncontent"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			mem, err := store.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if !mem.CreatedAt.Equal(tc.created) || !mem.UpdatedAt.Equal(tc.updated) {
				t.Fatalf("timestamps = %v, %v; want %v, %v", mem.CreatedAt, mem.UpdatedAt, tc.created, tc.updated)
			}
		})
	}
}
