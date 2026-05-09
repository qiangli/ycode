package netscan

import (
	"context"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// DefaultServices is the mDNS service set queried by Discover when the
// caller doesn't override. _ssh is the headline target; the rest are
// included because most LAN servers advertise at least one of them and
// it costs nothing to ask.
var DefaultServices = []string{
	"_ssh._tcp",
	"_workstation._tcp",
	"_smb._tcp",
	"_http._tcp",
	"_https._tcp",
}

// MDNSBrowse runs an mDNS browse for each service in `services`
// concurrently, blocks until `timeout` elapses or the context is
// cancelled, and returns the deduped Host list. Errors from the
// underlying lookup are swallowed and reported as zero-length results
// — mDNS browse is best-effort by nature.
//
// hashicorp/mdns delivers a *ServiceEntry on the Entries channel as soon
// as it sees an answer and then KEEPS MUTATING that same struct as more
// DNS records arrive (A, AAAA, TXT, SRV come in separately and merge
// into one entry). Reading any field of *ServiceEntry while Query is
// still running races the library's internal writes.
//
// We defend by:
//  1. While Query runs, only buffer the pointers — never touch *e.
//  2. After Query returns, mdns is done mutating; snapshot fields then.
//
// Trade-off: we lose progressive (streaming) results — every browse
// surfaces results only after its timeout fires. mDNS browses are
// short-lived (default 3s) so this is acceptable; correctness wins.
func MDNSBrowse(ctx context.Context, services []string, timeout time.Duration) []Host {
	if len(services) == 0 {
		services = DefaultServices
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	type observation struct {
		host    string
		ip      string
		port    int
		txt     []string
		service string
	}
	obsCh := make(chan observation, 256)
	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		svc := svc
		go func() {
			defer wg.Done()
			ch := make(chan *mdns.ServiceEntry, 64)
			var (
				collected []*mdns.ServiceEntry
				drainDone = make(chan struct{})
			)
			// Drain pointers without touching any field — mdns may
			// still be writing through *e at this moment.
			go func() {
				defer close(drainDone)
				for e := range ch {
					collected = append(collected, e)
				}
			}()
			params := &mdns.QueryParam{
				Service:             svc,
				Domain:              "local",
				Timeout:             timeout,
				Entries:             ch,
				WantUnicastResponse: false,
				DisableIPv4:         false,
				DisableIPv6:         true, // IPv6 is a v2 follow-up
			}
			_ = mdns.Query(params)
			close(ch)
			<-drainDone

			// Query has returned; mdns is no longer touching *e.
			// Now safe to read every field.
			for _, e := range collected {
				if e == nil {
					continue
				}
				obs := observation{
					host:    e.Host,
					ip:      pickIPFromEntry(e),
					port:    e.Port,
					service: svc,
				}
				if n := len(e.InfoFields); n > 0 {
					obs.txt = make([]string, n)
					copy(obs.txt, e.InfoFields)
				}
				select {
				case obsCh <- obs:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(obsCh) }()

	now := time.Now().UTC()
	hosts := make([]Host, 0, 8)
	for o := range obsCh {
		if o.ip == "" {
			continue
		}
		h := Host{
			Name:     o.host,
			IP:       o.ip,
			Port:     o.port,
			Service:  o.service,
			Sources:  []Source{SourceMDNS},
			LastSeen: now,
		}
		if len(o.txt) > 0 {
			h.Attrs = make(map[string]string, len(o.txt))
			for _, f := range o.txt {
				k, v := splitTXT(f)
				if k != "" {
					h.Attrs[k] = v
				}
			}
		}
		hosts = append(hosts, h)
	}
	return Merge(nil, hosts)
}

// pickIPFromEntry snapshots the IP off a *mdns.ServiceEntry. Must run
// only inside the relay goroutine while mdns is still writing — but
// even then, the read-of-pointer + .String() is a single point in time
// and avoids the longer window where the consumer might hold the entry
// through later iterations.
//
// The bare-pointer pickIP wrapper is kept for backwards compatibility
// with any external caller; new code should snapshot fields instead.
func pickIPFromEntry(e *mdns.ServiceEntry) string {
	if e == nil {
		return ""
	}
	if v := e.AddrV4; v != nil {
		return v.String()
	}
	if v := e.AddrV6; v != nil {
		return v.String()
	}
	return ""
}

// pickIP is the legacy entry point. Prefer pickIPFromEntry inside the
// relay goroutine; this exists only for any out-of-package callers.
func pickIP(e *mdns.ServiceEntry) string { return pickIPFromEntry(e) }

// splitTXT parses a TXT key=value field.
func splitTXT(f string) (string, string) {
	for i := 0; i < len(f); i++ {
		if f[i] == '=' {
			return f[:i], f[i+1:]
		}
	}
	return f, ""
}
