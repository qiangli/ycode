// Package kv is a backwards-compatibility shim that re-exports
// github.com/qiangli/ycode/pkg/memex/store/kv.
//
// Deprecated: use github.com/qiangli/ycode/pkg/memex/store/kv. This shim will
// be removed in Phase 6 of the memex refactor.
package kv

import (
	pkgkv "github.com/qiangli/ycode/pkg/memex/store/kv"
)

type Store = pkgkv.Store

// Open creates or opens a bbolt database at the given directory.
func Open(dir string) (*Store, error) { return pkgkv.Open(dir) }
