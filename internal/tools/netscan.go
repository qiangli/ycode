package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qiangli/ycode/internal/runtime/netscan"
)

// RegisterNetscanHandler wires the `netscan` tool. The tool exposes
// network host discovery (mDNS + system probes + optional TCP scan)
// to the LLM as a read-only operation; SSH connections are not opened
// from the agentic flow because they need a TTY — for non-interactive
// remote command execution, the LLM should use the `bash` tool with
// `ssh user@host -- '<cmd>'`.
func RegisterNetscanHandler(r *Registry) {
	if spec, ok := r.Get("netscan"); ok {
		spec.Handler = handleNetscan
	}
}

type netscanInput struct {
	Subnets   []string `json:"subnets,omitempty"`
	Services  []string `json:"services,omitempty"`
	ScanPorts bool     `json:"scan_ports,omitempty"`
	TimeoutMs int      `json:"timeout_ms,omitempty"`
}

func handleNetscan(ctx context.Context, input json.RawMessage) (string, error) {
	var params netscanInput
	if len(input) > 0 {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse netscan input: %w", err)
		}
	}
	timeout := time.Duration(params.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	// netscan.Discover handles the multi-subnet case via separate
	// passes; for the LLM tool we pick the first subnet (or auto-
	// detect) — the agentic use case rarely needs multi-CIDR.
	cidr := ""
	if len(params.Subnets) > 0 {
		cidr = params.Subnets[0]
	}

	hosts, err := netscan.Discover(ctx, netscan.Options{
		Services:       params.Services,
		CIDR:           cidr,
		EnablePortScan: params.ScanPorts,
		Timeout:        timeout,
	})
	if err != nil {
		return "", fmt.Errorf("netscan discover: %w", err)
	}

	out, err := json.MarshalIndent(map[string]any{
		"hosts": hosts,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	return string(out), nil
}
