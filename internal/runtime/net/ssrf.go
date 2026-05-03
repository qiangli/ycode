package net

import (
	"fmt"
	"net"
	"net/url"
	"os"
)

// blockedRanges contains CIDR ranges for private/internal networks.
var blockedRanges []*net.IPNet

func init() {
	cidrs := []string{
		// Loopback.
		"127.0.0.0/8",
		// Private (RFC 1918).
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		// Link-local / cloud metadata (AWS, GCP, Azure).
		"169.254.0.0/16",
		// CGNAT / Shared Address Space (RFC 6598).
		"100.64.0.0/10",
		// Reserved / "this network" (RFC 791).
		"0.0.0.0/8",
		// TEST-NET blocks (RFC 5737) — documentation ranges, never routable.
		"192.0.2.0/24",
		"198.51.100.0/24",
		"203.0.113.0/24",
		// Benchmarking (RFC 2544).
		"198.18.0.0/15",
		// Multicast (RFC 5771).
		"224.0.0.0/4",
		// IPv6 loopback.
		"::1/128",
		// IPv6 unique local.
		"fc00::/7",
		// IPv6 link-local.
		"fe80::/10",
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %s: %v", cidr, err))
		}
		blockedRanges = append(blockedRanges, ipNet)
	}
}

// isPrivateIP reports whether ip falls within any blocked range.
func isPrivateIP(ip net.IP) bool {
	for _, ipNet := range blockedRanges {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURL checks if a URL is safe to fetch (not targeting private networks).
// Returns an error if the URL resolves to a private/internal IP address.
// Set YCODE_ALLOW_PRIVATE_NETWORK=true to bypass the check (for development).
func ValidateURL(rawURL string) error {
	if os.Getenv("YCODE_ALLOW_PRIVATE_NETWORK") == "true" {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("blocked: URL has no hostname")
	}

	// If the host is already an IP address, check it directly.
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("blocked: URL resolves to private network address (%s)", ip)
		}
		return nil
	}

	// Resolve hostname to IP addresses.
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("blocked: URL resolves to private network address (%s)", ip)
		}
	}

	return nil
}
