// Package sqlite is a backwards-compatibility shim that re-exports
// github.com/qiangli/ycode/pkg/memex/store/sqlite.
//
// Deprecated: use github.com/qiangli/ycode/pkg/memex/store/sqlite. This shim
// will be removed in Phase 6 of the memex refactor.
package sqlite

import (
	pkgsqlite "github.com/qiangli/ycode/pkg/memex/store/sqlite"
)

type (
	Store     = pkgsqlite.Store
	Migration = pkgsqlite.Migration
)

// Open creates or opens a SQLite database at the given directory.
func Open(dir string) (*Store, error) { return pkgsqlite.Open(dir) }
