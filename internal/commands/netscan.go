package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/qiangli/ycode/internal/runtime/netscan"
)

// netscanHandler powers the embedded /netscan slash command. It is a
// first-class peer of /init and /commit — invoked from the TUI / web
// REPL the same way, with the same handler signature, no markdown
// skill file required.
//
// Behavior in this v1:
//
//   - "/netscan" or "/netscan list"   → discover and print a table.
//   - "/netscan json"                 → discover and print JSON.
//   - "/netscan scan"                 → like list but with TCP scan on.
//
// Connecting to a chosen host from the slash command is a Phase-2
// follow-up: the long-lived SSH session needs the server-side Manager
// + bus protocol so the connection can persist across TUI reconnects
// (the user's "non-blocking, switchable" requirement). Until that
// landing, users discover via /netscan and connect via the existing
// `/bash ssh user@host` path.
func netscanHandler(_ *RuntimeDeps) func(context.Context, string) (string, error) {
	return func(ctx context.Context, args string) (string, error) {
		mode, rest := parseNetscanArgs(args)
		_ = rest

		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()

		hosts, err := netscan.Discover(ctx, netscan.Options{
			EnablePortScan: mode == "scan",
			Timeout:        3 * time.Second,
		})
		if err != nil {
			return "", fmt.Errorf("discover: %w", err)
		}
		if len(hosts) == 0 {
			return "no hosts discovered (try `/netscan scan` for a TCP CONNECT pass)", nil
		}

		if mode == "json" {
			out, err := json.MarshalIndent(map[string]any{"hosts": hosts}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out), nil
		}

		var b strings.Builder
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "#\tNAME\tIP\tPORT\tSERVICE\tSOURCES\tLAST SEEN")
		for i, h := range hosts {
			port := ""
			if h.Port > 0 {
				port = fmt.Sprintf("%d", h.Port)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				i+1, h.Name, h.IP, port, h.Service,
				joinSourcesCmd(h.Sources), h.LastSeen.Local().Format("15:04:05"))
		}
		_ = tw.Flush()
		b.WriteString("\nTo connect: run `ycode netscan` outside the TUI, or `/bash ssh user@<ip>`.\n")
		return b.String(), nil
	}
}

func parseNetscanArgs(args string) (mode, rest string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "list", ""
	}
	parts := strings.Fields(args)
	switch parts[0] {
	case "list", "json", "scan":
		return parts[0], strings.Join(parts[1:], " ")
	default:
		return "list", args
	}
}

func joinSourcesCmd(s []netscan.Source) string {
	out := make([]string, 0, len(s))
	for _, src := range s {
		out = append(out, string(src))
	}
	return strings.Join(out, ",")
}
