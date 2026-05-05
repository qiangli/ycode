// Package memos is a backwards-compatibility shim that re-exports
// github.com/qiangli/ycode/pkg/memex/memos.
//
// Deprecated: use github.com/qiangli/ycode/pkg/memex/memos. This shim will be
// removed in Phase 6 of the memex refactor.
package memos

import (
	"net/http"

	pkgmemos "github.com/qiangli/ycode/pkg/memex/memos"
	store "github.com/qiangli/ycode/pkg/memex/store"
)

// Public types (data + interface).
type (
	Store        = pkgmemos.Store
	Memo         = pkgmemos.Memo
	MemoProperty = pkgmemos.MemoProperty
	ListOptions  = pkgmemos.ListOptions
	ListResult   = pkgmemos.ListResult
	SQLStore     = pkgmemos.SQLStore
)

// Constructors.
func NewSQLStore(db store.SQLStore) *SQLStore { return pkgmemos.NewSQLStore(db) }
func NewWebHandler(s Store) http.Handler      { return pkgmemos.NewWebHandler(s) }
