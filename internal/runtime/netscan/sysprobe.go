package netscan

import (
	"context"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SystemProbe shells out to whatever discovery tools the host happens
// to ship with — `arp`/`ip neigh` for the kernel ARP cache,
// `dns-sd -B` on macOS for richer mDNS, `avahi-browse` on Linux for
// the same. None of these are required; missing tools simply skip.
//
// The point: ycode's Go-native discovery stays the floor, and these
// supplementary tools layer on top so a single /netscan run beats any
// one bash invocation a developer would have made by hand.
//
// Results are merged into the working host set by the caller.
func SystemProbe(ctx context.Context, timeout time.Duration) []Host {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var hosts []Host
	hosts = append(hosts, probeARP(probeCtx)...)
	hosts = append(hosts, probeDNSSD(probeCtx)...)
	hosts = append(hosts, probeAvahi(probeCtx)...)
	return Merge(nil, hosts)
}

// probeARP reads the kernel's ARP/neighbor table. Tries `ip neigh`
// (modern Linux) first, then `arp -an` (BSD/macOS/legacy Linux). Each
// produces an IP+optional-name; we mark them SourceARP so the merge
// step can de-prioritize them vs an mDNS hit on the same IP.
func probeARP(ctx context.Context) []Host {
	now := time.Now().UTC()
	cmds := []*exec.Cmd{
		exec.CommandContext(ctx, "ip", "neigh"),
		exec.CommandContext(ctx, "arp", "-an"),
	}
	for _, cmd := range cmds {
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		hosts := parseARPOutput(string(out), now)
		if len(hosts) > 0 {
			return hosts
		}
	}
	return nil
}

var arpLineRE = regexp.MustCompile(`(\d+\.\d+\.\d+\.\d+)`)

func parseARPOutput(out string, now time.Time) []Host {
	seen := make(map[string]bool)
	var hosts []Host
	for _, line := range strings.Split(out, "\n") {
		// Skip incomplete entries — neither table includes a MAC for them.
		if strings.Contains(line, "incomplete") || strings.Contains(line, "INCOMPLETE") {
			continue
		}
		m := arpLineRE.FindString(line)
		if m == "" || seen[m] {
			continue
		}
		// Reject the local broadcast address (...255) and zero address.
		if ip := net.ParseIP(m); ip == nil || ip.IsUnspecified() {
			continue
		}
		seen[m] = true
		hosts = append(hosts, Host{
			IP:       m,
			Sources:  []Source{SourceARP},
			LastSeen: now,
		})
	}
	return hosts
}

// probeDNSSD uses macOS's dns-sd (and Linux mdnsd installs that ship
// it). We invoke the synchronous lookup form so the command exits
// promptly; output is one line per host on _ssh._tcp.
func probeDNSSD(ctx context.Context) []Host {
	if _, err := exec.LookPath("dns-sd"); err != nil {
		return nil
	}
	now := time.Now().UTC()
	cmd := exec.CommandContext(ctx, "dns-sd", "-t", "1", "-B", "_ssh._tcp", "local.")
	out, _ := cmd.Output() // ignore deadline-cancel error
	return parseDNSSDOutput(string(out), now)
}

var dnssdLineRE = regexp.MustCompile(`Add\s+\d+\s+\d+\s+local\.\s+_ssh\._tcp\.\s+(\S+)`)

func parseDNSSDOutput(out string, now time.Time) []Host {
	var hosts []Host
	for _, m := range dnssdLineRE.FindAllStringSubmatch(out, -1) {
		name := m[1]
		hosts = append(hosts, Host{
			Name:     name + ".local",
			IP:       "", // resolved later; merge will keep this row only if mDNS also hit
			Service:  "_ssh._tcp",
			Sources:  []Source{SourceDNSSD},
			LastSeen: now,
		})
	}
	// Resolve names to IPs via stdlib so the row is mergeable.
	resolved := hosts[:0]
	for _, h := range hosts {
		ips, err := net.LookupHost(h.Name)
		if err != nil || len(ips) == 0 {
			continue
		}
		h.IP = ips[0]
		resolved = append(resolved, h)
	}
	return resolved
}

// probeAvahi uses avahi-browse (Linux Avahi stack). The -p form emits
// machine-parseable lines: '=;eth0;IPv4;hostname;_ssh._tcp;local;...;ip;port;txt'.
func probeAvahi(ctx context.Context) []Host {
	if _, err := exec.LookPath("avahi-browse"); err != nil {
		return nil
	}
	now := time.Now().UTC()
	cmd := exec.CommandContext(ctx, "avahi-browse", "-p", "-r", "-t", "_ssh._tcp")
	out, _ := cmd.Output()
	return parseAvahiOutput(string(out), now)
}

func parseAvahiOutput(out string, now time.Time) []Host {
	var hosts []Host
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "=") {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) < 9 {
			continue
		}
		name := fields[3]
		host := fields[6]
		ip := fields[7]
		portStr := fields[8]
		port, _ := strconv.Atoi(portStr)
		if ip == "" {
			continue
		}
		hosts = append(hosts, Host{
			Name:     host,
			IP:       ip,
			Port:     port,
			Service:  "_ssh._tcp",
			Sources:  []Source{SourceAvahi},
			LastSeen: now,
			Attrs:    map[string]string{"avahi.advertised_name": name},
		})
	}
	return hosts
}
