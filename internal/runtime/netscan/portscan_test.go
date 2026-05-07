package netscan

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPortScan_HitsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hosts, err := PortScan(ctx, "127.0.0.0/30", []int{port}, 200*time.Millisecond, 8)
	if err != nil {
		t.Fatalf("PortScan: %v", err)
	}
	found := false
	for _, h := range hosts {
		if h.IP == "127.0.0.1" && h.Port == port {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("scan should have found 127.0.0.1:%d, got %+v", port, hosts)
	}
}

func TestPortScan_InvalidCIDR(t *testing.T) {
	_, err := PortScan(context.Background(), "not-a-cidr", []int{22}, 100*time.Millisecond, 4)
	if err == nil || !strings.Contains(err.Error(), "parse cidr") {
		t.Errorf("expected parse-cidr error, got %v", err)
	}
}

func TestLocalCIDR_ReturnsSlash24(t *testing.T) {
	cidr, err := LocalCIDR()
	if err != nil {
		// On a host with no non-loopback interface (rare in CI sandbox)
		// this is expected — skip rather than fail.
		t.Skipf("no local interface available: %v", err)
	}
	if !strings.HasSuffix(cidr, "/24") {
		t.Errorf("expected /24, got %q", cidr)
	}
}

func TestEnumerateIPv4_TinyRange(t *testing.T) {
	_, ipnet, _ := net.ParseCIDR("10.1.2.0/30")
	ips := enumerateIPv4(ipnet)
	want := []string{"10.1.2.0", "10.1.2.1", "10.1.2.2", "10.1.2.3"}
	if len(ips) != len(want) {
		t.Fatalf("got %v, want %v", ips, want)
	}
	for i, ip := range ips {
		if ip != want[i] {
			t.Errorf("ip %d: got %q, want %q", i, ip, want[i])
		}
	}
}
