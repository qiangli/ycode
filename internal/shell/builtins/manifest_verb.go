package builtins

import (
	"context"
	"encoding/json"
	"fmt"
)

func init() { Register(&manifestVerb{}) }

type manifestVerb struct{}

func (manifestVerb) Name() string        { return "manifest" }
func (manifestVerb) Description() string { return "Emit JSON capability catalog (built-ins, skills, sentinels, hints)" }
func (manifestVerb) Usage() string       { return "yc manifest" }

func (manifestVerb) Run(_ context.Context, _ []string, stdio Stdio, _ string) (int, error) {
	// We don't have the *ShellRuntime here (it's not passed to verbs in
	// the skeleton signature). Build a minimal manifest from the verb
	// registry alone — same shape as `ycode shell --manifest`, but the
	// runtime-derived fields (skills, slash commands) are absent.
	//
	// For full manifest content (skills + slash commands) callers should
	// use `ycode shell --manifest`. This in-shell verb is a quick
	// reflection on what's compiled in.
	out := struct {
		Version  string             `json:"version"`
		Builtins []verbManifestRow  `json:"builtins"`
	}{
		Version: "0.1.0",
	}
	for _, v := range All() {
		out.Builtins = append(out.Builtins, verbManifestRow{
			Name:        "yc " + v.Name(),
			Verb:        v.Name(),
			Description: v.Description(),
			Usage:       v.Usage(),
		})
	}
	enc := json.NewEncoder(stdio.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return 1, fmt.Errorf("encode manifest: %w", err)
	}
	return 0, nil
}

type verbManifestRow struct {
	Name        string `json:"name"`
	Verb        string `json:"verb"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
}
