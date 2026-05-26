package docs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// catalogRaw is the embedded YAML data file shipped with the binary.
// Same offline-discovery contract as agent/*.md — no filesystem reads
// at runtime, no network, no init() side effects.
//
//go:embed catalog.yaml
var catalogRaw []byte

// CatalogRow is one task entry in the capability catalog. Surfaces
// is a fixed three-key map (cli / yc / mcp) — each value is a list of
// invocation strings. Empty surface lists are allowed: a task may
// legitimately exist on only one surface (e.g. `yc sandbox` has no
// cobra or MCP face today).
type CatalogRow struct {
	Task     string              `yaml:"task" json:"task"`
	Surfaces map[string][]string `yaml:"surfaces" json:"surfaces"`
	Prereq   string              `yaml:"prereq,omitempty" json:"prereq,omitempty"`
	ReadMore string              `yaml:"read_more,omitempty" json:"read_more,omitempty"`
}

// Catalog is the parsed catalog.yaml file.
type Catalog struct {
	Rows []CatalogRow `yaml:"rows" json:"rows"`
}

var (
	catalogOnce sync.Once
	catalog     Catalog
	catalogErr  error
)

// LoadCatalog parses the embedded catalog.yaml exactly once and
// returns the cached result on subsequent calls. The catalog is
// intentionally process-lifetime cached — a binary's catalog is
// immutable across a run.
func LoadCatalog() (Catalog, error) {
	catalogOnce.Do(func() {
		if err := yaml.Unmarshal(catalogRaw, &catalog); err != nil {
			catalogErr = fmt.Errorf("docs: parse catalog.yaml: %w", err)
			return
		}
		for i, r := range catalog.Rows {
			if r.Task == "" {
				catalogErr = fmt.Errorf("docs: catalog row %d missing task", i)
				return
			}
			for surf := range r.Surfaces {
				switch surf {
				case "cli", "yc", "mcp":
				default:
					catalogErr = fmt.Errorf("docs: row %q unknown surface %q (allowed: cli, yc, mcp)", r.Task, surf)
					return
				}
			}
		}
	})
	return catalog, catalogErr
}

// FilterByTask returns rows whose task or read_more field substring-
// matches the query (case-insensitive). Matching read_more is what
// lets `--task browser` find the "drive a web page" row whose
// read_more is `browser`. Empty query returns all rows.
func (c Catalog) FilterByTask(query string) Catalog {
	if query == "" {
		return c
	}
	q := strings.ToLower(query)
	out := Catalog{}
	for _, r := range c.Rows {
		if strings.Contains(strings.ToLower(r.Task), q) ||
			strings.Contains(strings.ToLower(r.ReadMore), q) {
			out.Rows = append(out.Rows, r)
		}
	}
	return out
}

// RenderText writes a human-and-agent-readable catalog dump to w.
// Layout deliberately matches the form an external reviewer suggested
// as the "missing link" between the help index and per-tool
// descriptions: one block per row, surfaces aligned, prereq inline,
// read_more pointing at the deeper topic.
func (c Catalog) RenderText(w io.Writer) error {
	for i, r := range c.Rows {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "task: %s\n", r.Task); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "  surfaces:"); err != nil {
			return err
		}
		for _, key := range []string{"cli", "yc", "mcp"} {
			vals := r.Surfaces[key]
			if len(vals) == 0 {
				continue
			}
			if _, err := fmt.Fprintf(w, "    %-4s %s\n", key+":", strings.Join(vals, ", ")); err != nil {
				return err
			}
		}
		if r.Prereq != "" {
			if _, err := fmt.Fprintf(w, "  prereq: %s\n", r.Prereq); err != nil {
				return err
			}
		}
		if r.ReadMore != "" {
			if _, err := fmt.Fprintf(w, "  read_more: ycode docs %s\n", r.ReadMore); err != nil {
				return err
			}
		}
	}
	return nil
}

// RenderJSON writes the catalog as JSON. Stable key ordering via the
// struct tags so an agent diffing two runs sees a clean diff.
func (c Catalog) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}
