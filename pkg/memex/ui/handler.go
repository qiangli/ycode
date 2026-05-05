// Package ui exposes the memos web handler as a tiny convenience helper for
// embedders that want to mount the wiki UI without importing
// pkg/memex/memos directly.
package ui

import (
	"net/http"

	"github.com/qiangli/ycode/pkg/memex/memos"
)

// NewHandler returns the memos REST + embedded HTML UI handler bound to the
// given store. Mount it on any HTTP mux at the path prefix of your choice
// (the existing memos routes are root-relative and stay unchanged).
func NewHandler(store memos.Store) http.Handler {
	return memos.NewWebHandler(store)
}
