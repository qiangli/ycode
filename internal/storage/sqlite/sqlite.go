// Package sqlite provides a SQLite-backed SQL store using modernc.org/sqlite.
//
// This is a pure Go SQLite implementation (no CGO) with WAL mode enabled
// for concurrent read access. The database is stored as a single file
// with schema managed through versioned migrations.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/storage"

	_ "modernc.org/sqlite" // Pure Go SQLite driver.
)

// Store implements storage.SQLStore backed by SQLite.
type Store struct {
	db         *sql.DB
	migrations []Migration
}

// Migration is a versioned schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Open creates or opens a SQLite database at the given directory.
func Open(dir string) (*Store, error) {
	dbPath := filepath.Join(dir, "ycode.db")
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_cache_size=-64000&_foreign_keys=ON", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Connection pool tuning for SQLite.
	// SQLite serializes writes, so limit open connections to avoid contention.
	// One writer + a few readers is optimal for WAL mode.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(0) // Connections don't expire.

	// Verify connection.
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// Set PRAGMAs explicitly (DSN params may not be honored by all drivers).
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -64000",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(context.Background(), p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set %s: %w", p, err)
		}
	}

	s := &Store{
		db:         db,
		migrations: defaultMigrations(),
	}

	return s, nil
}

// Exec executes a statement that doesn't return rows.
func (s *Store) Exec(ctx context.Context, query string, args ...any) (storage.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

// QueryRow executes a query that returns at most one row.
func (s *Store) QueryRow(ctx context.Context, query string, args ...any) storage.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// Query executes a query that returns rows.
func (s *Store) Query(ctx context.Context, query string, args ...any) (storage.Rows, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Tx runs a function within a transaction.
func (s *Store) Tx(ctx context.Context, fn func(tx storage.SQLStore) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txStore := &txStore{tx: sqlTx}
	if err := fn(txStore); err != nil {
		sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}

// Migrate runs pending schema migrations.
func (s *Store) Migrate(ctx context.Context) error {
	// Create migration tracking table.
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS _migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Get current version.
	var currentVersion int
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM _migrations`).Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("get current version: %w", err)
	}

	// Sort migrations by version.
	sorted := make([]Migration, len(s.migrations))
	copy(sorted, s.migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	// Apply pending migrations.
	for _, m := range sorted {
		if m.Version <= currentVersion {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO _migrations (version, name) VALUES (?, ?)`,
			m.Version, m.Name,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}

	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for advanced use cases.
func (s *Store) DB() *sql.DB {
	return s.db
}

// txStore wraps a sql.Tx to implement storage.SQLStore.
type txStore struct {
	tx *sql.Tx
}

func (t *txStore) Exec(ctx context.Context, query string, args ...any) (storage.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

func (t *txStore) QueryRow(ctx context.Context, query string, args ...any) storage.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *txStore) Query(ctx context.Context, query string, args ...any) (storage.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (t *txStore) Tx(_ context.Context, fn func(tx storage.SQLStore) error) error {
	// Nested transactions use savepoints.
	return fn(t)
}

func (t *txStore) Migrate(_ context.Context) error {
	return fmt.Errorf("cannot run migrations inside a transaction")
}

func (t *txStore) Close() error { return nil }

// defaultMigrations returns the initial schema migrations.
func defaultMigrations() []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "initial_schema",
			SQL: strings.Join([]string{
				// Sessions table.
				`CREATE TABLE IF NOT EXISTS sessions (
					id         TEXT PRIMARY KEY,
					title      TEXT NOT NULL DEFAULT '',
					model      TEXT NOT NULL DEFAULT '',
					created_at TEXT NOT NULL DEFAULT (datetime('now')),
					updated_at TEXT NOT NULL DEFAULT (datetime('now')),
					summary    TEXT NOT NULL DEFAULT '',
					token_input  INTEGER NOT NULL DEFAULT 0,
					token_output INTEGER NOT NULL DEFAULT 0
				)`,

				// Messages table.
				`CREATE TABLE IF NOT EXISTS messages (
					id         TEXT PRIMARY KEY,
					session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
					role       TEXT NOT NULL,
					content    TEXT NOT NULL,
					model      TEXT NOT NULL DEFAULT '',
					timestamp  TEXT NOT NULL DEFAULT (datetime('now')),
					token_input  INTEGER NOT NULL DEFAULT 0,
					token_output INTEGER NOT NULL DEFAULT 0
				)`,

				// Messages indexes.
				`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id)`,
				`CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp)`,

				// Tasks table.
				`CREATE TABLE IF NOT EXISTS tasks (
					id          TEXT PRIMARY KEY,
					session_id  TEXT REFERENCES sessions(id) ON DELETE SET NULL,
					description TEXT NOT NULL,
					status      TEXT NOT NULL DEFAULT 'pending',
					output      TEXT NOT NULL DEFAULT '',
					error       TEXT NOT NULL DEFAULT '',
					created_at  TEXT NOT NULL DEFAULT (datetime('now')),
					updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
				)`,

				`CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id)`,
				`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,

				// Tool usage metrics.
				`CREATE TABLE IF NOT EXISTS tool_usage (
					id         INTEGER PRIMARY KEY AUTOINCREMENT,
					session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
					tool_name  TEXT NOT NULL,
					duration_ms INTEGER NOT NULL DEFAULT 0,
					success    INTEGER NOT NULL DEFAULT 1,
					timestamp  TEXT NOT NULL DEFAULT (datetime('now'))
				)`,

				`CREATE INDEX IF NOT EXISTS idx_tool_usage_tool ON tool_usage(tool_name)`,
				`CREATE INDEX IF NOT EXISTS idx_tool_usage_session ON tool_usage(session_id)`,

				// Prompt cache table.
				`CREATE TABLE IF NOT EXISTS prompt_cache (
					fingerprint TEXT PRIMARY KEY,
					response    TEXT NOT NULL,
					model       TEXT NOT NULL DEFAULT '',
					created_at  TEXT NOT NULL DEFAULT (datetime('now')),
					expires_at  TEXT NOT NULL
				)`,

				`CREATE INDEX IF NOT EXISTS idx_prompt_cache_expires ON prompt_cache(expires_at)`,
			}, ";\n"),
		},
	}
}
