package outcome

import (
	"encoding/base64"

	ghlib "github.com/google/go-github/v84/github"

	"github.com/qiangli/ycode/internal/runtime/github"
)

// b64 is stdlib base64 std encoding wrapped for brevity at the
// call-site in outcome.go.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// tokenFromClient recovers the GitHub auth token. The go-github
// client doesn't expose its token directly, so we re-walk the same
// precedence list the authenticated NewClient used. Argument is kept
// for API symmetry — a future implementation could read the token
// out of a custom RoundTripper. Empty when no token is discoverable
// (the no-auth case is handled by the caller as local-only).
func tokenFromClient(_ *ghlib.Client) string {
	return github.ResolveToken()
}
