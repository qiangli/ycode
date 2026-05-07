package netscan

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// DefaultScanPorts is the conservative set of ports the CONNECT scanner
// probes when the caller doesn't supply its own list. Skewed toward
// "is there a server here?" rather than full port enumeration.
var DefaultScanPorts = []int{22, 2222, 80, 443, 8080}

// PortScan runs a TCP CONNECT scan over every IP in `cidr` against
// `ports`, with `timeout` per dial and at most `concurrency` outstanding
// dials. A non-zero result is recorded as one Host per (IP, port) hit.
// IPv4 only.
//
// Pass an empty cidr to auto-detect the local /24 from the first
// non-loopback IPv4 interface.
func PortScan(ctx context.Context, cidr string, ports []int, timeout time.Duration, concurrency int) ([]Host, error) {
	if len(ports) == 0 {
		ports = DefaultScanPorts
	}
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 64
	}
	if cidr == "" {
		var err error
		cidr, err = LocalCIDR()
		if err != nil {
			return nil, err
		}
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse cidr %q: %w", cidr, err)
	}

	ips := enumerateIPv4(ipnet)
	sem := make(chan struct{}, concurrency)
	var (
		mu    sync.Mutex
		hosts []Host
		wg    sync.WaitGroup
	)
	for _, ip := range ips {
		for _, port := range ports {
			ip, port := ip, port
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()

				addr := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
				dialer := net.Dialer{Timeout: timeout}
				conn, err := dialer.DialContext(ctx, "tcp", addr)
				if err != nil {
					return
				}
				_ = conn.Close()

				mu.Lock()
				hosts = append(hosts, Host{
					IP:       ip,
					Port:     port,
					Sources:  []Source{SourceConnect},
					LastSeen: time.Now().UTC(),
				})
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	return Merge(nil, hosts), nil
}

// LocalCIDR returns the /24 covering the host's first non-loopback
// IPv4 address. Falls back to "192.168.1.0/24" if nothing is found
// (best-effort — the caller can override).
func LocalCIDR() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			// Force /24 for the scan even if the interface mask is
			// wider — scanning a full /16 by CONNECT is infeasible,
			// and most home/office networks fit a /24 anyway.
			return fmt.Sprintf("%d.%d.%d.0/24", ip4[0], ip4[1], ip4[2]), nil
		}
	}
	return "", fmt.Errorf("no non-loopback IPv4 interface found")
}

// enumerateIPv4 returns every IPv4 address in ipnet (including network
// and broadcast — the scan dialer will simply fail on those).
func enumerateIPv4(ipnet *net.IPNet) []string {
	out := []string{}
	for ip := ipnet.IP.Mask(ipnet.Mask).To4(); ip != nil && ipnet.Contains(ip); ip = nextIP(ip) {
		out = append(out, ip.String())
	}
	return out
}

func nextIP(ip net.IP) net.IP {
	cp := make(net.IP, len(ip))
	copy(cp, ip)
	for i := len(cp) - 1; i >= 0; i-- {
		cp[i]++
		if cp[i] != 0 {
			return cp
		}
	}
	return nil
}
