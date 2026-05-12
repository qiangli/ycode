package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandler_ServesCanvasShell guards the canvas/ subdirectory embed.
// If the directory layout breaks (renamed/deleted/forgotten asset)
// this fails loudly instead of silently 404'ing in the browser.
func TestHandler_ServesCanvasShell(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	cases := []struct {
		path string
		want string // substring expected in body
	}{
		{"/canvas/", "ycode canvas"}, // index.html title
		{"/canvas/", "prompt-form"},  // human input bar present
		{"/canvas/index.html", "ycode canvas"},
		{"/canvas/canvas.css", "--accent:"},     // CSS variable signature
		{"/canvas/canvas.js", "state.update"},   // canvas → bus dispatch the script subscribes to
		{"/canvas/canvas.js", "message.send"},   // human → bus prompt path the script publishes on
		{"/canvas/canvas.js", "resolveSession"}, // /api/status session adoption — humans driving /canvas/ join the /ycode/ session
		{"/canvas/a2ui.js", "applyOps"},         // vanilla A2UI renderer wired into canvas.js
		{"/canvas/a2ui.js", "updateDataModel"},  // proves the renderer handles the v0.9 op set, not just createSurface
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("body missing %q; got first 200 bytes: %q", tc.want, head(string(body), 200))
			}
		})
	}
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
