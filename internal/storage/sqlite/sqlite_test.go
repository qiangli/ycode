package sqlite

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/storage"
)

func TestOpen(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Run migrations twice -- second run should be a no-op.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate 1: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate 2: %v", err)
	}
}

func TestCRUD(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	t.Run("InsertAndQuerySession", func(t *testing.T) {
		_, err := s.Exec(ctx,
			`INSERT INTO sessions (id, title, model) VALUES (?, ?, ?)`,
			"sess-1", "Test Session", "claude-sonnet",
		)
		if err != nil {
			t.Fatalf("Insert session: %v", err)
		}

		var title string
		err = s.QueryRow(ctx, `SELECT title FROM sessions WHERE id = ?`, "sess-1").Scan(&title)
		if err != nil {
			t.Fatalf("Query session: %v", err)
		}
		if title != "Test Session" {
			t.Errorf("title = %q, want %q", title, "Test Session")
		}
	})

	t.Run("InsertAndQueryMessage", func(t *testing.T) {
		_, err := s.Exec(ctx,
			`INSERT INTO messages (id, session_id, role, content) VALUES (?, ?, ?, ?)`,
			"msg-1", "sess-1", "user", "Hello world",
		)
		if err != nil {
			t.Fatalf("Insert message: %v", err)
		}

		rows, err := s.Query(ctx, `SELECT id, role, content FROM messages WHERE session_id = ?`, "sess-1")
		if err != nil {
			t.Fatalf("Query messages: %v", err)
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			var id, role, content string
			if err := rows.Scan(&id, &role, &content); err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if content != "Hello world" {
				t.Errorf("content = %q, want %q", content, "Hello world")
			}
			count++
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("Rows err: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})

	t.Run("Transaction", func(t *testing.T) {
		err := s.Tx(ctx, func(tx storage.SQLStore) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO sessions (id, title, model) VALUES (?, ?, ?)`,
				"sess-tx", "TX Session", "model",
			)
			return err
		})
		if err != nil {
			t.Fatalf("Tx: %v", err)
		}

		var title string
		err = s.QueryRow(ctx, `SELECT title FROM sessions WHERE id = ?`, "sess-tx").Scan(&title)
		if err != nil {
			t.Fatalf("Query after Tx: %v", err)
		}
		if title != "TX Session" {
			t.Errorf("title = %q, want %q", title, "TX Session")
		}
	})

	t.Run("ToolUsage", func(t *testing.T) {
		_, err := s.Exec(ctx,
			`INSERT INTO tool_usage (session_id, tool_name, duration_ms, success) VALUES (?, ?, ?, ?)`,
			"sess-1", "Bash", 150, 1,
		)
		if err != nil {
			t.Fatalf("Insert tool_usage: %v", err)
		}

		var count int
		err = s.QueryRow(ctx, `SELECT COUNT(*) FROM tool_usage WHERE tool_name = ?`, "Bash").Scan(&count)
		if err != nil {
			t.Fatalf("Count tool_usage: %v", err)
		}
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}
	})
}

func TestCascadeDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Insert session + message.
	s.Exec(ctx, `INSERT INTO sessions (id, title, model) VALUES ('s1', 'test', 'model')`)
	s.Exec(ctx, `INSERT INTO messages (id, session_id, role, content) VALUES ('m1', 's1', 'user', 'hi')`)

	// Delete session -- messages should cascade.
	s.Exec(ctx, `DELETE FROM sessions WHERE id = 's1'`)

	var count int
	s.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = 's1'`).Scan(&count)
	if count != 0 {
		t.Errorf("messages after cascade delete = %d, want 0", count)
	}
}
