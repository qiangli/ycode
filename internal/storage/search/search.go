// Package search is a backwards-compatibility shim that re-exports
// github.com/qiangli/ycode/pkg/memex/store/search.
//
// Deprecated: use github.com/qiangli/ycode/pkg/memex/store/search. This shim
// will be removed in Phase 6 of the memex refactor.
package search

import (
	pkgsearch "github.com/qiangli/ycode/pkg/memex/store/search"
)

type Store = pkgsearch.Store

// Open creates a Bleve-backed search store at the given directory.
func Open(dir string) (*Store, error) { return pkgsearch.Open(dir) }
