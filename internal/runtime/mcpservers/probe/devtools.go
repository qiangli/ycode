package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/tracing"
	"github.com/chromedp/chromedp"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// Ring-buffer caps for events captured during a session. Sized to keep
// the result payload bounded — a flooded page won't blow up the
// agent's context.
const (
	maxNetEntries     = 200
	maxConsoleEntries = 200
)

// netEntry is the agent-facing shape of a network response. Only
// fields that survive truncation/serialization across the agent
// boundary live here.
type netEntry struct {
	URL      string `json:"url"`
	Method   string `json:"method,omitempty"`
	Status   int    `json:"status"`
	MIMEType string `json:"mime_type,omitempty"`
	Resource string `json:"resource_type,omitempty"`
	When     string `json:"when"`
}

type consoleEntry struct {
	Level string `json:"level"`
	Text  string `json:"text"`
	When  string `json:"when"`
}

// traceState carries everything the doPerfStart/Stop pair needs to
// know about an active recording. Only the count of dataCollected
// events crosses back to the agent — the raw trace can run to
// hundreds of MB and is useless to an LLM directly.
type traceState struct {
	mu         sync.Mutex
	active     bool
	startedAt  time.Time
	eventCount int
}

// devtools is the long-lived state for the per-target listener. One
// instance per Service. The listener runs on chromedp's internal
// goroutines so every field is guarded.
type devtools struct {
	netMu    sync.Mutex
	netRing  []netEntry
	conMu    sync.Mutex
	conRing  []consoleEntry
	trace    traceState
	enabled  bool
	enableMu sync.Mutex
}

func (d *devtools) recordNet(ev *network.EventResponseReceived) {
	if ev == nil || ev.Response == nil {
		return
	}
	d.netMu.Lock()
	defer d.netMu.Unlock()
	entry := netEntry{
		URL:      ev.Response.URL,
		Status:   int(ev.Response.Status),
		MIMEType: ev.Response.MimeType,
		Resource: string(ev.Type),
		When:     time.Now().UTC().Format(time.RFC3339),
	}
	d.netRing = append(d.netRing, entry)
	if len(d.netRing) > maxNetEntries {
		d.netRing = d.netRing[len(d.netRing)-maxNetEntries:]
	}
}

func (d *devtools) recordConsole(ev *runtime.EventConsoleAPICalled) {
	if ev == nil {
		return
	}
	parts := make([]string, 0, len(ev.Args))
	for _, arg := range ev.Args {
		if arg == nil {
			continue
		}
		if len(arg.Value) > 0 {
			parts = append(parts, strings.Trim(string(arg.Value), `"`))
			continue
		}
		if arg.Description != "" {
			parts = append(parts, arg.Description)
		}
	}
	d.conMu.Lock()
	defer d.conMu.Unlock()
	d.conRing = append(d.conRing, consoleEntry{
		Level: string(ev.Type),
		Text:  strings.Join(parts, " "),
		When:  time.Now().UTC().Format(time.RFC3339),
	})
	if len(d.conRing) > maxConsoleEntries {
		d.conRing = d.conRing[len(d.conRing)-maxConsoleEntries:]
	}
}

func (d *devtools) recordException(ev *runtime.EventExceptionThrown) {
	if ev == nil || ev.ExceptionDetails == nil {
		return
	}
	text := ev.ExceptionDetails.Text
	if ev.ExceptionDetails.Exception != nil && ev.ExceptionDetails.Exception.Description != "" {
		text = ev.ExceptionDetails.Exception.Description
	}
	d.conMu.Lock()
	defer d.conMu.Unlock()
	d.conRing = append(d.conRing, consoleEntry{
		Level: "exception",
		Text:  text,
		When:  time.Now().UTC().Format(time.RFC3339),
	})
	if len(d.conRing) > maxConsoleEntries {
		d.conRing = d.conRing[len(d.conRing)-maxConsoleEntries:]
	}
}

func (d *devtools) recordTraceData(ev *tracing.EventDataCollected) {
	if ev == nil {
		return
	}
	d.trace.mu.Lock()
	defer d.trace.mu.Unlock()
	if d.trace.active {
		d.trace.eventCount += len(ev.Value)
	}
}

// installListeners enables the Network + Runtime domains on the
// attached target and registers one chromedp listener that fans out
// to the typed recorders above. Safe to call once per Service.
func (d *devtools) installListeners(ctx context.Context) error {
	d.enableMu.Lock()
	defer d.enableMu.Unlock()
	if d.enabled {
		return nil
	}
	if err := chromedp.Run(ctx,
		network.Enable(),
		runtime.Enable(),
	); err != nil {
		return fmt.Errorf("probe: enable network+runtime: %w", err)
	}
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			d.recordNet(e)
		case *runtime.EventConsoleAPICalled:
			d.recordConsole(e)
		case *runtime.EventExceptionThrown:
			d.recordException(e)
		case *tracing.EventDataCollected:
			d.recordTraceData(e)
		}
	})
	d.enabled = true
	return nil
}

// --- ActionPerfStart / ActionPerfStop ---------------------------------------

func (s *Service) doPerfStart(ctx context.Context) (*mcpservers.BrowserResult, error) {
	s.dev.trace.mu.Lock()
	if s.dev.trace.active {
		s.dev.trace.mu.Unlock()
		return &mcpservers.BrowserResult{Error: "perf_start: trace already active — call perf_stop first"}, nil
	}
	s.dev.trace.active = true
	s.dev.trace.startedAt = time.Now()
	s.dev.trace.eventCount = 0
	s.dev.trace.mu.Unlock()

	if err := chromedp.Run(ctx, tracing.Start().
		WithTransferMode(tracing.TransferModeReportEvents)); err != nil {
		s.dev.trace.mu.Lock()
		s.dev.trace.active = false
		s.dev.trace.mu.Unlock()
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("perf_start: %v", err)}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: "tracing started"}, nil
}

func (s *Service) doPerfStop(ctx context.Context) (*mcpservers.BrowserResult, error) {
	s.dev.trace.mu.Lock()
	if !s.dev.trace.active {
		s.dev.trace.mu.Unlock()
		return &mcpservers.BrowserResult{Error: "perf_stop: no active trace"}, nil
	}
	startedAt := s.dev.trace.startedAt
	s.dev.trace.mu.Unlock()

	if err := chromedp.Run(ctx, tracing.End()); err != nil {
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("perf_stop: %v", err)}, nil
	}
	// Give the final dataCollected events a moment to drain through
	// the listener; CDP buffers them after End() returns. 250ms is
	// enough for short traces, won't matter for long ones.
	time.Sleep(250 * time.Millisecond)

	s.dev.trace.mu.Lock()
	count := s.dev.trace.eventCount
	s.dev.trace.active = false
	s.dev.trace.mu.Unlock()

	summary, _ := json.Marshal(map[string]any{
		"duration_ms": time.Since(startedAt).Milliseconds(),
		"event_count": count,
		"note":        "raw trace events are dropped after counting — perf_stop returns aggregate only",
	})
	return &mcpservers.BrowserResult{Success: true, Data: string(summary)}, nil
}

// --- ActionNetworkList ------------------------------------------------------

func (s *Service) doNetworkList() (*mcpservers.BrowserResult, error) {
	s.dev.netMu.Lock()
	defer s.dev.netMu.Unlock()
	out, _ := json.Marshal(map[string]any{
		"count":   len(s.dev.netRing),
		"entries": s.dev.netRing,
	})
	return &mcpservers.BrowserResult{Success: true, Data: string(out)}, nil
}

// --- ActionConsoleGet -------------------------------------------------------

func (s *Service) doConsoleGet() (*mcpservers.BrowserResult, error) {
	s.dev.conMu.Lock()
	defer s.dev.conMu.Unlock()
	out, _ := json.Marshal(map[string]any{
		"count":   len(s.dev.conRing),
		"entries": s.dev.conRing,
	})
	return &mcpservers.BrowserResult{Success: true, Data: string(out)}, nil
}

// --- ActionLighthouse -------------------------------------------------------

// lighthouseScript pulls every Performance API datapoint the browser
// has already collected (navigation timing, paint, LCP, CLS, FCP).
// Returns JSON the agent can read directly.
//
// Scope honesty: this is not Lighthouse-the-tool. Lighthouse runs
// custom controlled workloads (throttling profiles, lab-mode device
// emulation, multiple categories — perf, a11y, SEO, best-practices,
// PWA) and depends on a Node runtime — neither of which fit ycode's
// single-binary, pure-Go wedge. The action ships Core Web Vitals +
// navigation timing because they answer 80% of "is this page fast?"
// in <50 lines of JS and zero new dependencies. For full
// Lighthouse, the user runs `lighthouse <url>` externally.
const lighthouseScript = `
(() => {
	const out = {
		mode: "core-web-vitals",
		paint: {},
		navigation: {},
		largest_contentful_paint_ms: null,
		cumulative_layout_shift: null,
		first_input_delay_ms: null,
		resource_count: 0,
		notes: []
	};
	try {
		for (const p of performance.getEntriesByType('paint')) {
			out.paint[p.name.replace(/-/g, '_')] = Math.round(p.startTime);
		}
		const nav = performance.getEntriesByType('navigation')[0];
		if (nav) {
			out.navigation = {
				ttfb_ms: Math.round(nav.responseStart - nav.requestStart),
				dom_content_loaded_ms: Math.round(nav.domContentLoadedEventEnd),
				load_event_ms: Math.round(nav.loadEventEnd),
				transfer_size: nav.transferSize,
				encoded_body_size: nav.encodedBodySize,
				type: nav.type
			};
		}
		const lcps = performance.getEntriesByType('largest-contentful-paint');
		if (lcps && lcps.length) {
			out.largest_contentful_paint_ms = Math.round(lcps[lcps.length - 1].startTime);
		}
		let cls = 0;
		for (const ls of performance.getEntriesByType('layout-shift')) {
			if (!ls.hadRecentInput) cls += ls.value;
		}
		out.cumulative_layout_shift = Number(cls.toFixed(4));
		const fids = performance.getEntriesByType('first-input');
		if (fids && fids.length) {
			out.first_input_delay_ms = Math.round(fids[0].processingStart - fids[0].startTime);
		}
		out.resource_count = performance.getEntriesByType('resource').length;
		if (out.largest_contentful_paint_ms === null) {
			out.notes.push('LCP not yet observed — observer needs to have run since page load. Navigate and wait a beat.');
		}
		if (out.first_input_delay_ms === null) {
			out.notes.push('FID not observed — fires only on the first user interaction.');
		}
	} catch (e) {
		out.error = String(e);
	}
	return JSON.stringify(out);
})()
`

func (s *Service) doLighthouse(ctx context.Context) (*mcpservers.BrowserResult, error) {
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(lighthouseScript, &raw)); err != nil {
		return &mcpservers.BrowserResult{Error: fmt.Sprintf("lighthouse: %v", err)}, nil
	}
	return &mcpservers.BrowserResult{Success: true, Data: raw}, nil
}
