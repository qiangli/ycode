package main

import (
	"net/http"
	"net/http/pprof"
)

// debugPprofMux returns a ServeMux that exposes the standard library's
// net/http/pprof handlers under /debug/pprof/. It's intentionally a fresh
// mux (not http.DefaultServeMux) so the proxy doesn't accidentally expose
// anything else that package init() may have wired up there.
//
// Why this exists: when `ycode serve` misbehaves (CPU loop, goroutine leak,
// heap growth), the only available signal previously was `ps` and macOS
// `sample`. With pprof exposed via the observability proxy, debugging is
// a curl away — exactly what the embedded OTEL stack is supposed to enable.
func debugPprofMux() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/debug/pprof/", pprof.Index)
	m.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	m.HandleFunc("/debug/pprof/profile", pprof.Profile)
	m.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	m.HandleFunc("/debug/pprof/trace", pprof.Trace)
	// Named profiles. pprof.Handler(name) doesn't care about the URL —
	// it just streams the named profile in pprof format.
	for _, name := range []string{"goroutine", "heap", "allocs", "threadcreate", "block", "mutex"} {
		m.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
	return m
}
