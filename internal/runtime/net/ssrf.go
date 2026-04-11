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
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // private
		"172.16.0.0/12",  // private
		"192.168.0.0/16", // private
		"169.254.0.0/16", // link-local / cloud metadata
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
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
