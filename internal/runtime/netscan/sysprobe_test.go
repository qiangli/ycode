package netscan

import (
	"testing"
	"time"
)

func TestParseARPOutput_LinuxIPNeigh(t *testing.T) {
	out := `192.168.1.1 dev en0 lladdr aa:bb:cc:dd:ee:ff REACHABLE
192.168.1.42 dev en0 lladdr 00:11:22:33:44:55 STALE
192.168.1.99 dev en0  INCOMPLETE
`
	hosts := parseARPOutput(out, time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC))
	if len(hosts) != 2 {
		t.Fatalf("got %d, want 2 (incomplete row should be filtered)", len(hosts))
	}
	for _, want := range []string{"192.168.1.1", "192.168.1.42"} {
		found := false
		for _, h := range hosts {
			if h.IP == want {
				found = true
				if !h.HasSource(SourceARP) {
					t.Errorf("%s missing arp source", want)
				}
			}
		}
		if !found {
			t.Errorf("expected %s in output", want)
		}
	}
}

func TestParseARPOutput_BSDArpAn(t *testing.T) {
	out := `? (192.168.1.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
? (192.168.1.50) at 00:11:22:33:44:55 on en0 ifscope [ethernet]
? (192.168.1.255) at (incomplete) on en0 ifscope [ethernet]
`
	hosts := parseARPOutput(out, time.Now().UTC())
	if len(hosts) != 2 {
		t.Fatalf("got %d entries, want 2; output: %v", len(hosts), hosts)
	}
}

func TestParseAvahiOutput(t *testing.T) {
	// Synthetic two-line avahi-browse output. The =-prefixed line is
	// the resolved record we parse.
	out := `+;eth0;IPv4;myserver;_ssh._tcp;local
=;eth0;IPv4;myserver;_ssh._tcp;local;myserver.local;192.168.1.42;22;
`
	hosts := parseAvahiOutput(out, time.Now().UTC())
	if len(hosts) != 1 {
		t.Fatalf("want 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.IP != "192.168.1.42" || h.Port != 22 || h.Service != "_ssh._tcp" {
		t.Errorf("unexpected host: %+v", h)
	}
	if !h.HasSource(SourceAvahi) {
		t.Errorf("missing avahi source: %v", h.Sources)
	}
}

func TestParseDNSSDOutput(t *testing.T) {
	// Without resolution this returns empty; the parser depends on
	// net.LookupHost succeeding for the discovered name. We test
	// that an unresolvable .local name is filtered out cleanly.
	out := `Browsing for _ssh._tcp.local
DATE: ---Wed 07 May 2026---
12:00:00.000  ...STARTING...
Timestamp     A/R Flags if Domain                    Service Type         Instance Name
12:00:00.001  Add     2 14 local.                    _ssh._tcp.           definitely-does-not-exist-host
`
	got := parseDNSSDOutput(out, time.Now().UTC())
	for _, h := range got {
		if h.IP == "" {
			t.Errorf("dns-sd parser should drop unresolvable names, got %+v", h)
		}
	}
}
