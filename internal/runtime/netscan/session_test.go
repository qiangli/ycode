package netscan

import (
	"bytes"
	"sync"
	"testing"
)

func TestRingBuffer_WriteSnapshotUnderWrap(t *testing.T) {
	r := newRingBuffer(8)
	if _, err := r.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := r.snapshot(); !bytes.Equal(got, []byte("hello")) {
		t.Errorf("snapshot before wrap: got %q want %q", got, "hello")
	}
	if _, err := r.Write([]byte("WORLD!")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// After wrap: ring holds last 8 bytes of "helloWORLD!" → "loWORLD!"
	if got := r.snapshot(); !bytes.Equal(got, []byte("loWORLD!")) {
		t.Errorf("snapshot after wrap: got %q want %q", got, "loWORLD!")
	}
}

func TestRingBuffer_FanOutSubscribers(t *testing.T) {
	r := newRingBuffer(64)
	a := r.subscribe()
	b := r.subscribe()
	defer r.unsubscribe(a)
	defer r.unsubscribe(b)

	var wg sync.WaitGroup
	wg.Add(2)
	got := make([][]byte, 2)
	for i, ch := range []chan []byte{a, b} {
		go func(i int, ch chan []byte) {
			defer wg.Done()
			got[i] = <-ch
		}(i, ch)
	}
	r.Write([]byte("ping"))
	wg.Wait()

	for i, g := range got {
		if !bytes.Equal(g, []byte("ping")) {
			t.Errorf("subscriber %d: got %q want %q", i, g, "ping")
		}
	}
}

func TestManager_ListAndCloseAll(t *testing.T) {
	// Build a Manager with synthetic sessions injected; Connect()
	// would dial a real SSH server which is exercised in the
	// integration-tagged e2e test (ssh_e2e_test.go).
	m := NewManager()
	m.sessions["s1"] = &Session{ID: "s1", Host: &Host{IP: "10.0.0.1"}, exitedC: make(chan struct{})}
	m.sessions["s2"] = &Session{ID: "s2", Host: &Host{IP: "10.0.0.2"}, exitedC: make(chan struct{})}

	if got := m.List(); len(got) != 2 {
		t.Errorf("List: got %d, want 2", len(got))
	}
	if s := m.Get("s1"); s == nil || s.Host.IP != "10.0.0.1" {
		t.Errorf("Get s1 lookup failed: %+v", s)
	}
	m.CloseAll()
	if got := m.List(); len(got) != 0 {
		t.Errorf("after CloseAll: got %d sessions, want 0", len(got))
	}
}
