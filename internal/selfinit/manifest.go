package selfinit

import (
	"path/filepath"
)

// DefaultPort is the proxy port `ycode serve` listens on by default.
//
// Chosen to sit below the OS ephemeral-port pool on both Linux
// (default ip_local_port_range 32768–60999) and macOS (49152–65535),
// so a fresh `ycode serve` cannot race-lose to an OS-assigned
// ephemeral socket. IANA-unassigned; pi mnemonic.
const DefaultPort = 31415

// ManifestPath returns the canonical location ycode serve writes its
// manifest to (~/.agents/ycode/manifest.json).
//
// SelfInit no longer reads the manifest. It used to derive a list of
// MCP servers from it and register those into foreign tools' configs;
// ycode retired MCP entirely (docs/plan-remove-mcp.md), so there is
// nothing left to advertise — advertising a server the binary does not
// serve is what made foreign CLIs fail at startup. The path is still
// exported because `ycode init --doctor` prints it and `ycode serve`
// writes it: the manifest remains the discovery file for the live HTTP
// API, just not for MCP.
func ManifestPath(home string) string {
	return filepath.Join(home, ".agents", "ycode", "manifest.json")
}
