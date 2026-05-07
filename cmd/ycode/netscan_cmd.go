package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/netscan"
)

// newNetscanCmd builds the `ycode netscan` cobra command. The command
// is the CLI surface of the netscan feature — discovery + interactive
// pick + SSH connect — while the LLM tool (`netscan` in
// internal/tools/) and the embedded slash command provide the other
// two surfaces. All three sit on internal/runtime/netscan/.
func newNetscanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "netscan",
		Short: "Discover servers on the local network and SSH to one",
		Long: `Probe the local network via mDNS (and optionally TCP CONNECT scan), opportunistically
augment with system tools (arp / ip neigh / dns-sd / avahi-browse) when present,
print a numbered table of discovered hosts, and SSH to the host you pick.

The discovery library merges every available source into a single deduped view —
this is strictly a superset of what running any one bash command would produce.

Use --list to list only without opening an SSH session, or --json for
machine-readable output.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scan, _ := cmd.Flags().GetBool("scan")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			port, _ := cmd.Flags().GetInt("port")
			userFlag, _ := cmd.Flags().GetString("user")
			listOnly, _ := cmd.Flags().GetBool("list")
			jsonOut, _ := cmd.Flags().GetBool("json")

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout+5*time.Second)
			defer cancel()

			hosts, err := netscan.Discover(ctx, netscan.Options{
				EnablePortScan: scan,
				Timeout:        timeout,
			})
			if err != nil {
				return fmt.Errorf("discover: %w", err)
			}
			if len(hosts) == 0 {
				fmt.Fprintln(os.Stderr, "no hosts discovered.")
				return nil
			}

			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"hosts": hosts})
			}
			printHosts(os.Stdout, hosts)
			if listOnly {
				return nil
			}

			idx, err := promptHostIndex(len(hosts))
			if err != nil {
				return err
			}
			if idx < 0 {
				return nil // user quit
			}
			h := hosts[idx]

			username := userFlag
			if username == "" {
				if u, err := user.Current(); err == nil {
					username = u.Username
				}
			}
			fmt.Fprintf(os.Stderr, "\nConnecting to %s@%s …\n", username, h.Display())
			return netscan.Interactive(ctx, &h, netscan.SSHOptions{
				User: username,
				Port: port,
			})
		},
	}
	cmd.Flags().Bool("scan", false, "Also do a TCP CONNECT scan over the local /24")
	cmd.Flags().Duration("timeout", 3*time.Second, "Discovery timeout")
	cmd.Flags().Int("port", 22, "SSH port")
	cmd.Flags().String("user", "", "SSH user (default: current OS user)")
	cmd.Flags().Bool("list", false, "List only — don't connect")
	cmd.Flags().Bool("json", false, "Output JSON instead of a table")
	return cmd
}

// printHosts renders the discovered host list as a numbered tabular
// view. Sources are joined to make the provenance visible at a glance.
func printHosts(w *os.File, hosts []netscan.Host) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tNAME\tIP\tPORT\tSERVICE\tSOURCES\tLAST SEEN")
	for i, h := range hosts {
		port := ""
		if h.Port > 0 {
			port = strconv.Itoa(h.Port)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i+1, h.Name, h.IP, port, h.Service, joinSources(h.Sources),
			h.LastSeen.Local().Format("15:04:05"))
	}
	_ = tw.Flush()
}

func joinSources(s []netscan.Source) string {
	out := make([]string, 0, len(s))
	for _, src := range s {
		out = append(out, string(src))
	}
	return strings.Join(out, ",")
}

// promptHostIndex reads "Select host [1-N, q to quit]: " from stdin
// and returns the zero-based index, or -1 on quit.
func promptHostIndex(n int) (int, error) {
	fmt.Fprintf(os.Stderr, "\nSelect host [1-%d, q to quit]: ", n)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return -1, scanner.Err()
	}
	s := strings.TrimSpace(scanner.Text())
	if s == "" || s == "q" || s == "Q" {
		return -1, nil
	}
	idx, err := strconv.Atoi(s)
	if err != nil || idx < 1 || idx > n {
		return -1, fmt.Errorf("invalid selection %q", s)
	}
	return idx - 1, nil
}
