// Package netscan discovers hosts on the local network and connects to
// them over SSH. It is the engine behind the `netscan` LLM tool, the
// `ycode netscan` CLI command, and the embedded `/netscan` slash
// command — three thin shells over one library.
//
// Discovery uses multiple sources opportunistically and merges their
// results: mDNS (hashicorp/mdns), TCP CONNECT scanning over the local
// /24 (stdlib net), and system tools when available (arp / ip neigh /
// dns-sd / avahi-browse / nmap). Hosts are deduped by IP and sorted by
// most-recent-first.
package netscan

import (
	"net"
	"strings"
	"time"
)

// Source identifies how a Host was first observed. Used to weight
// confidence (mDNS > nmap > arp > cache) and to surface in the UI.
type Source string

const (
	SourceMDNS    Source = "mdns"
	SourceConnect Source = "connect-scan"
	SourceARP     Source = "arp"
	SourceDNSSD   Source = "dns-sd"
	SourceAvahi   Source = "avahi-browse"
	SourceNmap    Source = "nmap"
	SourceCache   Source = "cache"
)

// Host is a discovered network host. The same struct is the wire shape
// returned by the netscan LLM tool and the on-disk cache record.
type Host struct {
	// Name is the resolvable hostname (preferring fully-qualified mDNS
	// .local names) or empty if only an IP was observed.
	Name string `json:"name,omitempty"`

	// IP is the host's IPv4 or IPv6 address as a string. Always set —
	// the merge step keys hosts by IP.
	IP string `json:"ip"`

	// Port is the TCP port observed open or advertised. 0 means
	// unknown (e.g., an ARP entry without a service hit).
	Port int `json:"port,omitempty"`

	// Service is the mDNS service type ("_ssh._tcp"), an explicit
	// label from system probes ("ssh", "http"), or empty.
	Service string `json:"service,omitempty"`

	// Sources is every source that observed this host on this
	// discovery pass. Multiple sources increase confidence.
	Sources []Source `json:"sources"`

	// SeenCount is the lifetime number of discovery passes that have
	// observed this host. Persisted via the cache.
	SeenCount int `json:"seen_count,omitempty"`

	// LastSeen is the most recent observation (UTC).
	LastSeen time.Time `json:"last_seen"`

	// Attrs is free-form metadata harvested from probes (TXT records,
	// nmap service-version strings, etc.). Optional.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// HasSource reports whether s is among the host's sources.
func (h *Host) HasSource(s Source) bool {
	for _, v := range h.Sources {
		if v == s {
			return true
		}
	}
	return false
}

// AddSource appends s to Sources if it is not already present.
func (h *Host) AddSource(s Source) {
	if !h.HasSource(s) {
		h.Sources = append(h.Sources, s)
	}
}

// Display returns a short human-friendly identifier ("name (ip:port)"
// or just "ip:port" when the name is unknown).
func (h *Host) Display() string {
	addr := h.IP
	if h.Port > 0 {
		addr = net.JoinHostPort(h.IP, itoa(h.Port))
	}
	if h.Name != "" && h.Name != h.IP {
		return strings.TrimSuffix(h.Name, ".") + " (" + addr + ")"
	}
	return addr
}

// itoa avoids a strconv import for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
