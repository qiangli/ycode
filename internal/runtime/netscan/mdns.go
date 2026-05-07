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
func MDNSBrowse(ctx context.Context, services []string, timeout time.Duration) []Host {
	if len(services) == 0 {
		services = DefaultServices
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	type observation struct {
		entry   *mdns.ServiceEntry
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
			doneRelay := make(chan struct{})
			go func() {
				defer close(doneRelay)
				for e := range ch {
					select {
					case obsCh <- observation{entry: e, service: svc}:
					case <-ctx.Done():
						return
					}
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
			<-doneRelay
		}()
	}
	go func() { wg.Wait(); close(obsCh) }()

	now := time.Now().UTC()
	hosts := make([]Host, 0, 8)
	for o := range obsCh {
		ip := pickIP(o.entry)
		if ip == "" {
			continue
		}
		h := Host{
			Name:     o.entry.Host,
			IP:       ip,
			Port:     o.entry.Port,
			Service:  o.service,
			Sources:  []Source{SourceMDNS},
			LastSeen: now,
		}
		if len(o.entry.InfoFields) > 0 {
			h.Attrs = make(map[string]string, len(o.entry.InfoFields))
			for _, f := range o.entry.InfoFields {
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

// pickIP returns the entry's IPv4 if present, else the IPv6, else "".
func pickIP(e *mdns.ServiceEntry) string {
	if e == nil {
		return ""
	}
	if e.AddrV4 != nil {
		return e.AddrV4.String()
	}
	if e.AddrV6 != nil {
		return e.AddrV6.String()
	}
	return ""
}

// splitTXT parses a TXT key=value field.
func splitTXT(f string) (string, string) {
	for i := 0; i < len(f); i++ {
		if f[i] == '=' {
			return f[:i], f[i+1:]
		}
	}
	return f, ""
}
