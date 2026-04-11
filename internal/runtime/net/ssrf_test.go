package net

import (
	"net"
	"os"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"192.168.0.0", true},
		{"169.254.169.254", true},
		{"169.254.0.1", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"93.184.216.34", false},            // example.com
		{"172.32.0.1", false},               // just outside 172.16.0.0/12
		{"192.169.0.1", false},              // just outside 192.168.0.0/16
		{"2607:f8b0:4004:800::200e", false}, // Google public IPv6
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP %s", tt.ip)
		}
		got := isPrivateIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestValidateURL_BlocksPrivateIPs(t *testing.T) {
	// Ensure override is not set.
	os.Unsetenv("YCODE_ALLOW_PRIVATE_NETWORK")

	tests := []struct {
		url     string
		blocked bool
	}{
		{"http://127.0.0.1/secret", true},
		{"http://127.0.0.1:8080/path", true},
		{"http://169.254.169.254/latest/meta-data/", true},
		{"http://10.0.0.1/internal", true},
		{"http://192.168.1.1/admin", true},
		{"http://[::1]/path", true},
		{"http://[fe80::1]/path", true},
		{"http://[fc00::1]/path", true},
		{"http://8.8.8.8/dns", false},
		{"http://1.1.1.1/", false},
	}
	for _, tt := range tests {
		err := ValidateURL(tt.url)
		if tt.blocked && err == nil {
			t.Errorf("ValidateURL(%s) should be blocked but was allowed", tt.url)
		}
		if !tt.blocked && err != nil {
			t.Errorf("ValidateURL(%s) should be allowed but got error: %v", tt.url, err)
		}
	}
}

func TestValidateURL_AllowsPublicHostnames(t *testing.T) {
	os.Unsetenv("YCODE_ALLOW_PRIVATE_NETWORK")

	// example.com resolves to public IPs.
	err := ValidateURL("http://example.com/page")
	if err != nil {
		t.Errorf("ValidateURL(http://example.com/page) should be allowed but got: %v", err)
	}
}

func TestValidateURL_AllowPrivateNetworkOverride(t *testing.T) {
	os.Setenv("YCODE_ALLOW_PRIVATE_NETWORK", "true")
	defer os.Unsetenv("YCODE_ALLOW_PRIVATE_NETWORK")

	// With override, private IPs should be allowed.
	err := ValidateURL("http://127.0.0.1/secret")
	if err != nil {
		t.Errorf("ValidateURL with YCODE_ALLOW_PRIVATE_NETWORK=true should allow 127.0.0.1, got: %v", err)
	}

	err = ValidateURL("http://10.0.0.1/internal")
	if err != nil {
		t.Errorf("ValidateURL with YCODE_ALLOW_PRIVATE_NETWORK=true should allow 10.0.0.1, got: %v", err)
	}
}

func TestValidateURL_InvalidURLs(t *testing.T) {
	os.Unsetenv("YCODE_ALLOW_PRIVATE_NETWORK")

	err := ValidateURL("://bad")
	if err == nil {
		t.Error("ValidateURL with invalid URL should return error")
	}

	err = ValidateURL("http:///no-host")
	if err == nil {
		t.Error("ValidateURL with empty host should return error")
	}
}
