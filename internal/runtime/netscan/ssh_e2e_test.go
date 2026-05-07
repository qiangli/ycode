//go:build integration

package netscan

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestSession_E2E spins up an in-process SSH server, connects via the
// netscan Manager, sends a line of input, asserts the echoed output
// reaches the Session's ring buffer, and verifies clean teardown.
//
// This exercises the full pipeline: dial → handshake → PTY request →
// Shell start → stdin pipe → ring-buffered stdout → Send/Snapshot →
// Close. SSH-server logic is intentionally minimal (echo on every
// chunk) — the point is that *our* client/session plumbing is
// correct, not that we're a fully RFC-compliant SSH server.
func TestSession_E2E_SendAndSnapshot(t *testing.T) {
	// Force the auth chain to ignore any developer-machine ssh-agent
	// or default keys; the test must succeed with *only* the explicit
	// KeyPath we pass below.
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", t.TempDir())

	srv, err := newTestSSHServer(t)
	if err != nil {
		t.Fatalf("start test ssh server: %v", err)
	}
	defer srv.Close()

	hostKnown := writeKnownHosts(t, srv.Addr.IP.String(), srv.Addr.Port, srv.HostKey.PublicKey())
	keyPath := writeClientKey(t, srv.ClientPrivKey)

	mgr := NewManager()
	defer mgr.CloseAll()

	host := &Host{IP: srv.Addr.IP.String(), Port: srv.Addr.Port}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, sess, err := mgr.Connect(ctx, host, SSHOptions{
		User:           "test",
		KeyPath:        keyPath,
		KnownHostsPath: hostKnown,
		Timeout:        5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if id == "" || sess == nil {
		t.Fatalf("Connect returned id=%q sess=%v", id, sess)
	}
	if got := mgr.List(); len(got) != 1 || got[0].ID != id {
		t.Errorf("List: got %v, want one entry id=%s", got, id)
	}

	// /btw-style send: write to remote stdin without attaching the TTY.
	if err := sess.Send([]byte("hello world\n")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Poll the snapshot until the echoed bytes arrive (test server
	// echoes every chunk it sees on stdin).
	deadline := time.Now().Add(5 * time.Second)
	var snap []byte
	for time.Now().Before(deadline) {
		snap = sess.Snapshot()
		if bytes.Contains(snap, []byte("hello world")) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !bytes.Contains(snap, []byte("hello world")) {
		t.Errorf("snapshot did not capture echoed input; got %q", snap)
	}

	// Close via the Manager and confirm List shrinks to zero.
	if err := mgr.Close(id); err != nil {
		t.Errorf("Close: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.List()) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(mgr.List()) != 0 {
		t.Errorf("after Close, manager still has %d sessions", len(mgr.List()))
	}
}

// --- Test harness ---

type testSSHServer struct {
	Addr          *net.TCPAddr
	HostKey       ssh.Signer
	ClientPrivKey ed25519.PrivateKey
	listener      net.Listener
	stopOnce      sync.Once
	stopped       chan struct{}
}

func newTestSSHServer(t *testing.T) (*testSSHServer, error) {
	t.Helper()
	hostPub, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		return nil, err
	}
	_ = hostPub

	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	clientSshPub, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		return nil, err
	}
	authedFP := ssh.FingerprintSHA256(clientSshPub)

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if ssh.FingerprintSHA256(key) != authedFP {
				return nil, errors.New("unauthorized key")
			}
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	srv := &testSSHServer{
		Addr:          ln.Addr().(*net.TCPAddr),
		HostKey:       hostSigner,
		ClientPrivKey: clientPriv,
		listener:      ln,
		stopped:       make(chan struct{}),
	}

	go srv.serve(cfg)
	return srv, nil
}

func (s *testSSHServer) serve(cfg *ssh.ServerConfig) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go handleSSHConn(conn, cfg)
	}
}

func (s *testSSHServer) Close() {
	s.stopOnce.Do(func() {
		_ = s.listener.Close()
		close(s.stopped)
	})
}

// handleSSHConn implements just enough SSH server to satisfy our
// client: PTY request → Shell start → echo every byte read on the
// channel back out. Exit on close.
func handleSSHConn(c net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, ureqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer ch.Close()
			var shellStarted atomic.Bool
			go func() {
				for r := range ureqs {
					switch r.Type {
					case "pty-req":
						_ = r.Reply(true, nil)
					case "shell":
						_ = r.Reply(true, nil)
						shellStarted.Store(true)
					case "window-change":
						// no-op
					default:
						if r.WantReply {
							_ = r.Reply(false, nil)
						}
					}
				}
			}()
			// Wait briefly for shell start, then echo.
			deadline := time.Now().Add(2 * time.Second)
			for !shellStarted.Load() && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			buf := make([]byte, 4096)
			for {
				n, err := ch.Read(buf)
				if n > 0 {
					_, _ = ch.Write(buf[:n])
				}
				if err != nil {
					if !errors.Is(err, io.EOF) {
						return
					}
					return
				}
			}
		}()
	}
}

func writeKnownHosts(t *testing.T, ip string, port int, key ssh.PublicKey) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	host := fmt.Sprintf("[%s]:%d", ip, port)
	line := host + " " + key.Type() + " " + base64Pub(key) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

func base64Pub(key ssh.PublicKey) string {
	// crypto/ssh.MarshalAuthorizedKey returns a full line; chop off
	// the trailing newline and the leading "type " prefix to keep
	// only the base64-encoded key blob expected on a known_hosts line.
	auth := ssh.MarshalAuthorizedKey(key)
	auth = bytes.TrimSpace(auth)
	parts := bytes.SplitN(auth, []byte(" "), 2)
	if len(parts) != 2 {
		return string(auth)
	}
	return string(parts[1])
}

func writeClientKey(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "id_test")
	pemBlock, err := ssh.MarshalPrivateKey(priv, "netscan-test")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}
