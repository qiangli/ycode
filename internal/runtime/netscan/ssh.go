package netscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

// SSHOptions configures a connection attempt. Zero values use sensible
// defaults: current OS user, port 22, agent + default-key auth chain,
// strict known_hosts checking with a yes/no prompt for unknown hosts.
type SSHOptions struct {
	User    string        // default: $USER / current user
	Port    int           // default: 22
	Timeout time.Duration // default: 15s
	KeyPath string        // explicit key; empty = walk default key set
	// AllowPassword enables a password prompt as a last-resort auth
	// step. Off by default — the dial fails cleanly with no usable
	// auth method instead, which the caller can decide whether to retry.
	AllowPassword bool
	// KnownHostsPath overrides ~/.ssh/known_hosts. Empty = default.
	KnownHostsPath string
}

// Interactive opens an interactive SSH session to host using stdlib
// crypto/ssh. It allocates a PTY, switches the local terminal to raw
// mode, copies stdin/stdout/stderr through the channel, and restores
// the terminal on exit.
//
// The caller owns os.Stdin/os.Stdout — the TUI must release the
// terminal before invoking this (mirrors the bash TTYExecutor pattern
// in internal/cli/tty_exec.go).
func Interactive(ctx context.Context, host *Host, opts SSHOptions) error {
	client, err := dial(ctx, host, opts)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("netscan SSH requires a TTY on stdin")
	}
	width, height, err := term.GetSize(fd)
	if err != nil {
		width, height = 80, 24
	}
	prevState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer func() { _ = term.Restore(fd, prevState) }()

	termType := os.Getenv("TERM")
	if termType == "" {
		termType = "xterm-256color"
	}
	if err := session.RequestPty(termType, height, width, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return fmt.Errorf("request pty: %w", err)
	}

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	// session.Wait blocks until the remote shell exits. ssh.ExitMissing
	// (when the server closed without sending exit-status) is a normal
	// termination — surface it as nil. Other errors propagate so the
	// caller can show them after restoring the TUI.
	werr := session.Wait()
	var exitMissing *ssh.ExitMissingError
	if errors.As(werr, &exitMissing) {
		return nil
	}
	return werr
}

// Run executes a single non-interactive command on host and returns
// stdout/stderr concatenated. Useful from the LLM agentic flow
// where there's no TTY — exposed via the bash tool today, but kept
// here so future agentic-friendly callers (mesh, ralph) have a
// PTY-free entry point.
func Run(ctx context.Context, host *Host, opts SSHOptions, command string) (string, error) {
	client, err := dial(ctx, host, opts)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new ssh session: %w", err)
	}
	defer session.Close()
	out, err := session.CombinedOutput(command)
	return string(out), err
}

// dial resolves the connection target, builds the auth chain, applies
// known_hosts verification, and returns a connected *ssh.Client.
func dial(ctx context.Context, host *Host, opts SSHOptions) (*ssh.Client, error) {
	if host == nil || host.IP == "" {
		return nil, errors.New("netscan: host has no IP")
	}
	if opts.User == "" {
		if u, err := user.Current(); err == nil {
			opts.User = u.Username
		}
	}
	port := opts.Port
	if port == 0 {
		port = host.Port
	}
	if port == 0 {
		port = 22
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}

	auth, err := buildAuth(opts)
	if err != nil {
		return nil, err
	}
	if len(auth) == 0 {
		return nil, errors.New("netscan: no usable SSH auth methods (no agent, no default key, AllowPassword=false)")
	}

	hostKeyCb, err := buildHostKeyCallback(opts.KnownHostsPath)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCb,
		Timeout:         opts.Timeout,
	}

	addr := net.JoinHostPort(host.IP, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: opts.Timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// buildAuth returns the auth methods to try, in preference order:
// SSH agent, then explicit key (if provided), then default keys, then
// optional password prompt. Each method is a fallback — crypto/ssh
// tries them in order and stops at the first success.
func buildAuth(opts SSHOptions) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	keyPaths := defaultKeyPaths()
	if opts.KeyPath != "" {
		keyPaths = []string{expandHome(opts.KeyPath)}
	}
	for _, p := range keyPaths {
		signer, err := loadKey(p)
		if err != nil {
			continue
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if opts.AllowPassword {
		methods = append(methods, ssh.PasswordCallback(promptPassword))
	}
	return methods, nil
}

func defaultKeyPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	names := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(home, ".ssh", n))
	}
	return out
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// loadKey reads and parses a private key. If the key is passphrase-
// protected, prompts the user via the controlling TTY.
func loadKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return signer, nil
	}
	var passErr *ssh.PassphraseMissingError
	if !errors.As(err, &passErr) {
		return nil, err
	}
	pass, perr := promptPassphrase(filepath.Base(path))
	if perr != nil {
		return nil, perr
	}
	return ssh.ParsePrivateKeyWithPassphrase(data, pass)
}

func promptPassphrase(keyName string) ([]byte, error) {
	fmt.Fprintf(os.Stderr, "passphrase for %s: ", keyName)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return pw, err
}

func promptPassword() (string, error) {
	fmt.Fprint(os.Stderr, "password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(pw), err
}

// buildHostKeyCallback returns a HostKeyCallback that:
//   - accepts known good keys silently;
//   - prompts the user yes/no on first-seen hosts (TOFU) and appends
//     the key to known_hosts on yes;
//   - rejects on a key MISMATCH.
//
// If known_hosts can't be read at all, falls back to TOFU-only.
func buildHostKeyCallback(path string) (ssh.HostKeyCallback, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // fallback noted in caller
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if ferr == nil {
			_ = f.Close()
		}
	}
	hkcb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %s: %w", path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := hkcb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				// Unknown host — TOFU prompt, append on yes.
				return tofuPrompt(path, hostname, remote, key)
			}
			// Known host with mismatched key — fail loudly.
			return fmt.Errorf("host key MISMATCH for %s: expected %s but got %s — possible MITM",
				hostname, keyErr.Want[0].Key.Type(), key.Type())
		}
		return err
	}, nil
}

func tofuPrompt(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	fp := ssh.FingerprintSHA256(key)
	fmt.Fprintf(os.Stderr, "\nThe authenticity of host '%s (%s)' can't be established.\n", hostname, remote.String())
	fmt.Fprintf(os.Stderr, "%s key fingerprint is %s.\n", key.Type(), fp)
	fmt.Fprint(os.Stderr, "Are you sure you want to continue connecting (yes/no)? ")

	var ans string
	if _, err := fmt.Fscanln(os.Stdin, &ans); err != nil {
		return fmt.Errorf("read tofu response: %w", err)
	}
	if !strings.EqualFold(ans, "yes") && !strings.EqualFold(ans, "y") {
		return errors.New("host key not accepted")
	}
	return appendKnownHost(path, hostname, key)
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	_, err = io.WriteString(f, line+"\n")
	return err
}
