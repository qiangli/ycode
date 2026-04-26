package memos

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

const snippetMaxLen = 200

// SQLStore implements Store backed by ycode's SQLite database.
type SQLStore struct {
	db storage.SQLStore
}

// NewSQLStore creates a memo store using the given SQLite store.
func NewSQLStore(db storage.SQLStore) *SQLStore {
	return &SQLStore{db: db}
}

func (s *SQLStore) Healthy() bool { return s.db != nil }

func (s *SQLStore) Create(ctx context.Context, memo *Memo) error {
	if memo.ID == "" {
		memo.ID = newID()
	}
	if memo.Visibility == "" {
		memo.Visibility = "PRIVATE"
	}
	if memo.State == "" {
		memo.State = "NORMAL"
	}
	now := time.Now().UTC()
	memo.CreatedAt = now
	memo.UpdatedAt = now
	memo.Tags = extractTags(memo.Content)
	memo.Property = computeProperty(memo.Content)
	memo.Snippet = generateSnippet(memo.Content, snippetMaxLen)

	return s.db.Tx(ctx, func(tx storage.SQLStore) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO memos (id, content, visibility, state, pinned, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			memo.ID, memo.Content, memo.Visibility, memo.State,
			boolToInt(memo.Pinned), memo.CreatedAt.Format(time.RFC3339), memo.UpdatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("insert memo: %w", err)
		}
		return insertTags(ctx, tx, memo.ID, memo.Tags)
	})
}

func (s *SQLStore) Get(ctx context.Context, id string) (*Memo, error) {
	memo, err := scanMemo(s.db.QueryRow(ctx,
		`SELECT id, content, visibility, state, pinned, created_at, updated_at FROM memos WHERE id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("get memo %s: %w", id, err)
	}
	memo.Tags, _ = loadTags(ctx, s.db, id)
	return memo, nil
}

func (s *SQLStore) Update(ctx context.Context, id string, content string) (*Memo, error) {
	now := time.Now().UTC()
	tags := extractTags(content)

	err := s.db.Tx(ctx, func(tx storage.SQLStore) error {
		res, err := tx.Exec(ctx,
			`UPDATE memos SET content = ?, updated_at = ? WHERE id = ?`,
			content, now.Format(time.RFC3339), id,
		)
		if err != nil {
			return fmt.Errorf("update memo: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("memo %s not found", id)
		}
		// Replace tags.
		if _, err := tx.Exec(ctx, `DELETE FROM memo_tags WHERE memo_id = ?`, id); err != nil {
			return fmt.Errorf("delete old tags: %w", err)
		}
		return insertTags(ctx, tx, id, tags)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *SQLStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.Exec(ctx, `DELETE FROM memos WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete memo %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memo %s not found", id)
	}
	return nil
}

func (s *SQLStore) List(ctx context.Context, opts ListOptions) (*ListResult, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	limit := opts.PageSize + 1 // fetch one extra to detect next page

	var args []any
	query := `SELECT id, content, visibility, state, pinned, created_at, updated_at FROM memos`

	if opts.PageToken != "" {
		cursorTime, cursorID, ok := decodeCursor(opts.PageToken)
		if !ok {
			return nil, fmt.Errorf("invalid page token")
		}
		query += ` WHERE (created_at, id) < (?, ?)`
		args = append(args, cursorTime, cursorID)
	}

	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list memos: %w", err)
	}
	defer rows.Close()

	var memos []*Memo
	for rows.Next() {
		m, err := scanMemoRows(rows)
		if err != nil {
			return nil, err
		}
		memos = append(memos, m)
	}

	result := &ListResult{}
	if len(memos) > opts.PageSize {
		last := memos[opts.PageSize-1]
		result.NextPageToken = encodeCursor(last.CreatedAt.Format(time.RFC3339), last.ID)
		memos = memos[:opts.PageSize]
	}

	// Load tags for each memo.
	for _, m := range memos {
		m.Tags, _ = loadTags(ctx, s.db, m.ID)
	}
	result.Memos = memos
	return result, nil
}

func (s *SQLStore) Search(ctx context.Context, query string, maxResults int) ([]*Memo, error) {
	if maxResults <= 0 {
		maxResults = 20
	}

	// Use FTS5 for search.
	rows, err := s.db.Query(ctx,
		`SELECT m.id, m.content, m.visibility, m.state, m.pinned, m.created_at, m.updated_at
		 FROM memos m
		 JOIN memos_fts f ON m.rowid = f.rowid
		 WHERE memos_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		ftsQuery(query), maxResults,
	)
	if err != nil {
		// Fall back to LIKE if FTS fails (e.g., very short queries).
		return s.searchLike(ctx, query, maxResults)
	}
	defer rows.Close()

	memos, err := collectMemos(ctx, s.db, rows)
	if err != nil {
		return nil, err
	}
	if len(memos) == 0 {
		// FTS may miss some patterns; fall back to LIKE.
		return s.searchLike(ctx, query, maxResults)
	}
	return memos, nil
}

func (s *SQLStore) searchLike(ctx context.Context, query string, maxResults int) ([]*Memo, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, content, visibility, state, pinned, created_at, updated_at
		 FROM memos WHERE content LIKE ? ORDER BY created_at DESC LIMIT ?`,
		"%"+query+"%", maxResults,
	)
	if err != nil {
		return nil, fmt.Errorf("search memos: %w", err)
	}
	defer rows.Close()
	return collectMemos(ctx, s.db, rows)
}

func (s *SQLStore) SearchByTag(ctx context.Context, tag string, maxResults int) ([]*Memo, error) {
	if maxResults <= 0 {
		maxResults = 20
	}
	rows, err := s.db.Query(ctx,
		`SELECT m.id, m.content, m.visibility, m.state, m.pinned, m.created_at, m.updated_at
		 FROM memos m JOIN memo_tags t ON m.id = t.memo_id
		 WHERE t.tag = ? ORDER BY m.created_at DESC LIMIT ?`,
		strings.ToLower(tag), maxResults,
	)
	if err != nil {
		return nil, fmt.Errorf("search by tag: %w", err)
	}
	defer rows.Close()
	return collectMemos(ctx, s.db, rows)
}

// --- helpers ---

func insertTags(ctx context.Context, tx storage.SQLStore, memoID string, tags []string) error {
	for _, tag := range tags {
		if _, err := tx.Exec(ctx,
			`INSERT OR IGNORE INTO memo_tags (memo_id, tag) VALUES (?, ?)`,
			memoID, tag,
		); err != nil {
			return fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}
	return nil
}

func loadTags(ctx context.Context, db storage.SQLStore, memoID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT tag FROM memo_tags WHERE memo_id = ? ORDER BY tag`, memoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			continue
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func scanMemo(row storage.Row) (*Memo, error) {
	m := &Memo{}
	var createdAt, updatedAt string
	var pinned int
	if err := row.Scan(&m.ID, &m.Content, &m.Visibility, &m.State, &pinned, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, err
	}
	m.Pinned = pinned != 0
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	m.Property = computeProperty(m.Content)
	m.Snippet = generateSnippet(m.Content, snippetMaxLen)
	return m, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanMemoRows(row scannable) (*Memo, error) {
	m := &Memo{}
	var createdAt, updatedAt string
	var pinned int
	if err := row.Scan(&m.ID, &m.Content, &m.Visibility, &m.State, &pinned, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	m.Pinned = pinned != 0
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	m.Property = computeProperty(m.Content)
	m.Snippet = generateSnippet(m.Content, snippetMaxLen)
	return m, nil
}

func collectMemos(ctx context.Context, db storage.SQLStore, rows storage.Rows) ([]*Memo, error) {
	var memos []*Memo
	for rows.Next() {
		m, err := scanMemoRows(rows)
		if err != nil {
			return nil, err
		}
		m.Tags, _ = loadTags(ctx, db, m.ID)
		memos = append(memos, m)
	}
	return memos, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ftsQuery converts a user search string to an FTS5 query.
// Wraps each word in quotes for literal matching.
func ftsQuery(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " ")
}
