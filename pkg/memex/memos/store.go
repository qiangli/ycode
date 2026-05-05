// Package memos provides persistent memo storage for ycode's long-term memory.
//
// Memos are markdown notes with tags, visibility controls, and full-text search.
// The store is backed by SQLite (via ycode's storage.SQLStore) with FTS5 indexing.
package memos

import (
	"context"
	"time"
)

// Store is the interface for memo persistence.
type Store interface {
	Create(ctx context.Context, memo *Memo) error
	Get(ctx context.Context, id string) (*Memo, error)
	Update(ctx context.Context, id string, content string) (*Memo, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, opts ListOptions) (*ListResult, error)
	Search(ctx context.Context, query string, maxResults int) ([]*Memo, error)
	SearchByTag(ctx context.Context, tag string, maxResults int) ([]*Memo, error)
	Healthy() bool
}

// Memo represents a stored note.
type Memo struct {
	ID         string       `json:"id"`
	Content    string       `json:"content"`
	Visibility string       `json:"visibility"` // PRIVATE | PROTECTED | PUBLIC
	State      string       `json:"state"`      // NORMAL | ARCHIVED
	Tags       []string     `json:"tags"`
	Pinned     bool         `json:"pinned"`
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`
	Snippet    string       `json:"snippet"`
	Property   MemoProperty `json:"property"`
}

// MemoProperty holds computed properties of a memo's content.
type MemoProperty struct {
	HasLink            bool   `json:"hasLink"`
	HasTaskList        bool   `json:"hasTaskList"`
	HasCode            bool   `json:"hasCode"`
	HasIncompleteTasks bool   `json:"hasIncompleteTasks"`
	Title              string `json:"title"`
}

// ListOptions configures memo listing.
type ListOptions struct {
	PageSize  int
	PageToken string // opaque cursor for pagination
}

// ListResult is a paginated list of memos.
type ListResult struct {
	Memos         []*Memo
	NextPageToken string
}
