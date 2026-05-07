package netscan

import (
	"context"
	"sync"
	"time"
)

// Options controls a Discover pass. Zero values are sensible defaults.
type Options struct {
	// Services to query via mDNS. nil means DefaultServices.
	Services []string

	// CIDR to TCP CONNECT scan. Empty + EnablePortScan=true auto-
	// detects the local /24.
	CIDR string

	// EnablePortScan turns on the TCP CONNECT scanner. Off by default
	// because port-scanning a /24 is slow and noisy.
	EnablePortScan bool

	// ScanPorts overrides the default set when EnablePortScan is true.
	ScanPorts []int

	// Timeout bounds the whole pass. Default 3s.
	Timeout time.Duration

	// CachePath overrides the cache location. Empty means default
	// (~/.agents/ycode/netscan-cache.json). Set to "-" to disable.
	CachePath string
}

// Discover runs every available source in parallel (cache load,
// mDNS, system tools, optionally port scan), merges the results,
// and persists the cache. Returns the merged Host list sorted
// most-recent-first.
//
// Each source is independently best-effort — a missing system
// command, a broken mDNS multicast path, or an empty cache do not
// fail the call. The only error path is a fatal port-scan misconfig
// (invalid CIDR).
func Discover(ctx context.Context, opts Options) ([]Host, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var cache *Cache
	if opts.CachePath != "-" {
		cache = NewCache(opts.CachePath)
	}

	var (
		mu        sync.Mutex
		collected []Host
	)
	gather := func(src []Host) {
		mu.Lock()
		collected = append(collected, src...)
		mu.Unlock()
	}

	var (
		wg          sync.WaitGroup
		scanErr     error
		scanErrOnce sync.Once
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		gather(MDNSBrowse(ctx, opts.Services, opts.Timeout))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		gather(SystemProbe(ctx, opts.Timeout))
	}()

	if opts.EnablePortScan {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hosts, err := PortScan(ctx, opts.CIDR, opts.ScanPorts, 500*time.Millisecond, 64)
			if err != nil {
				scanErrOnce.Do(func() { scanErr = err })
				return
			}
			gather(hosts)
		}()
	}

	wg.Wait()

	var cached []Host
	if cache != nil {
		// Load is fast (one file read); doing it inline post-discovery
		// keeps the goroutine count down and avoids races on save.
		cached, _ = cache.Load()
	}

	merged := MergeAndStamp(cached, collected)

	if cache != nil {
		// Keep entries seen in the last 30 days; older ones drop off so
		// stale LANs don't accumulate forever.
		_ = cache.Save(merged, 30*24*time.Hour)
	}

	if scanErr != nil && len(merged) == 0 {
		return nil, scanErr
	}
	return merged, nil
}
