package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/qiangli/ycode/internal/features"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
)

// inYcodeSourceTree reports whether the current working directory looks like
// the ycode source repository (has go.mod naming module github.com/qiangli/ycode).
// Returns the repo root and true on match. The on-disk file-existence check in
// `features verify` is only meaningful from inside the source tree; running
// from a user's project would falsely report every internal file as missing.
func inYcodeSourceTree() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		mod := filepath.Join(dir, "go.mod")
		if f, err := os.Open(mod); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "module github.com/qiangli/ycode") {
					f.Close()
					return dir, true
				}
				if strings.HasPrefix(line, "module ") {
					break
				}
			}
			f.Close()
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func runFeatureInvocation(inv harnesscli.Invocation, streams harnesscli.IO) error {
	reg, err := features.Load()
	if err != nil {
		return err
	}
	switch inv.Dispatch.Action {
	case "list":
		tw := tabwriter.NewWriter(streams.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "TIER\tNAME\tFILES\tNOTES")
		for _, t := range []features.Tier{features.TierStable, features.TierExperimental, features.TierWIP} {
			for _, f := range reg.ByTier(t) {
				files := ""
				if len(f.Files) > 0 {
					files = f.Files[0]
					if len(f.Files) > 1 {
						files = fmt.Sprintf("%s (+%d)", files, len(f.Files)-1)
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Tier, f.Name, files, f.Notes)
			}
		}
		return tw.Flush()
	case "readme":
		readmeWrite, _ := inv.Flags["write"].(string)
		rendered := features.RenderReadmeFeatures(reg)
		if readmeWrite == "" {
			fmt.Fprint(streams.Out, rendered)
			return nil
		}
		changed, err := features.ReplaceSection(readmeWrite, rendered)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(streams.Out, "updated: %s\n", readmeWrite)
		} else {
			fmt.Fprintf(streams.Out, "up to date: %s\n", readmeWrite)
		}
		return nil
	case "verify":
		// Always validate registry structure (Load already calls Validate).
		// On-disk file check only when we're inside the ycode source tree;
		// running from any other repo would false-positive every entry.
		root, inSource := inYcodeSourceTree()
		if !inSource {
			fmt.Fprintf(streams.Out, "registry: structurally valid (%d features)\n", len(reg.Features))
			fmt.Fprintln(streams.Out, "note: on-disk file check skipped — not running inside the ycode source tree")
			return nil
		}
		issues := features.Verify(reg, root)
		for _, iss := range issues {
			fmt.Fprintln(streams.Err, iss)
		}
		if len(issues) > 0 {
			return fmt.Errorf("%d feature registry verification issue(s)", len(issues))
		}
		fmt.Fprintf(streams.Out, "registry: ok (%d features, all declared paths exist)\n", len(reg.Features))
		return nil
	default:
		return fmt.Errorf("unsupported feature action %q", inv.Dispatch.Action)
	}
}
