package dashboards

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// simplifiedProject is the structure used in default_project.json.
type simplifiedProject struct {
	Name       string                `json:"name"`
	Dashboards []simplifiedDashboard `json:"dashboards"`
}

type simplifiedDashboard struct {
	Name   string            `json:"name"`
	Panels []simplifiedPanel `json:"panels"`
}

type simplifiedPanel struct {
	Title string `json:"title"`
	Query string `json:"query"`
	Type  string `json:"type"` // timeseries, stat, table
}

// gridColumns is the total number of columns in a Perses grid layout.
const gridColumns = 24

// Provision writes Perses resources directly into the file database at dataDir.
// This bypasses Perses's schema validation (which requires plugin archives)
// and writes resources in the same format as the Perses file DAO.
//
// Database path layout:
//
//	dataDir/globaldatasources/<name>.json
//	dataDir/projects/<name>.json
//	dataDir/dashboards/<project>/<name>.json
//
// prometheusURL is the Prometheus query API URL (e.g. "http://127.0.0.1:58080/prometheus").
func Provision(dataDir, prometheusURL string) error {
	var projects []simplifiedProject
	if err := json.Unmarshal(DefaultProjectsJSON, &projects); err != nil {
		return fmt.Errorf("parse default projects: %w", err)
	}

	now := time.Now().UTC()

	// 1. Global datasource.
	ds := buildGlobalDatasource(prometheusURL, now)
	dsDir := filepath.Join(dataDir, "globaldatasources")
	if err := os.MkdirAll(dsDir, 0o755); err != nil {
		return err
	}
	if err := writeJSONIfMissing(filepath.Join(dsDir, "prometheus.json"), ds); err != nil {
		return err
	}

	// 2. Projects and dashboards.
	for _, p := range projects {
		projectSlug := slugify(p.Name)

		// Project.
		projDir := filepath.Join(dataDir, "projects")
		if err := os.MkdirAll(projDir, 0o755); err != nil {
			return err
		}
		proj := buildProject(projectSlug, p.Name, now)
		if err := writeJSONIfMissing(filepath.Join(projDir, projectSlug+".json"), proj); err != nil {
			return err
		}

		// Dashboards.
		dbDir := filepath.Join(dataDir, "dashboards", projectSlug)
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			return err
		}
		for _, d := range p.Dashboards {
			db := buildDashboard(projectSlug, d, now)
			dbSlug := slugify(d.Name)
			if err := writeJSONIfMissing(filepath.Join(dbDir, dbSlug+".json"), db); err != nil {
				return err
			}
		}

		// Seed empty directories for resource types the Perses UI
		// queries on project pages. Without these, the file DB returns
		// 404 instead of an empty list.
		for _, kind := range []string{
			"roles", "rolebindings", "secrets",
			"variables", "datasources", "folders",
			"ephemeraldashboards",
		} {
			_ = os.MkdirAll(filepath.Join(dataDir, kind, projectSlug), 0o755)
		}
	}

	return nil
}

func buildProject(slug, displayName string, now time.Time) map[string]any {
	return map[string]any{
		"kind": "Project",
		"metadata": map[string]any{
			"name":      slug,
			"createdAt": now.Format(time.RFC3339Nano),
			"updatedAt": now.Format(time.RFC3339Nano),
			"version":   0,
		},
		"spec": map[string]any{
			"display": map[string]any{"name": displayName},
		},
	}
}

func buildGlobalDatasource(prometheusURL string, now time.Time) map[string]any {
	return map[string]any{
		"kind": "GlobalDatasource",
		"metadata": map[string]any{
			"name":      "prometheus",
			"createdAt": now.Format(time.RFC3339Nano),
			"updatedAt": now.Format(time.RFC3339Nano),
			"version":   0,
		},
		"spec": map[string]any{
			"default": true,
			"plugin": map[string]any{
				"kind": "PrometheusDatasource",
				"spec": map[string]any{
					"proxy": map[string]any{
						"kind": "HTTPProxy",
						"spec": map[string]any{
							"url": prometheusURL,
						},
					},
				},
			},
		},
	}
}

func buildDashboard(project string, d simplifiedDashboard, now time.Time) map[string]any {
	panels := make(map[string]any)
	var items []any

	x, y, rowHeight := 0, 0, 0
	for i, p := range d.Panels {
		key := panelKey(i, p.Title)

		var width, height int
		var pluginKind string
		pluginSpec := map[string]any{}

		switch p.Type {
		case "stat":
			width = 6
			height = 4
			pluginKind = "StatChart"
			pluginSpec["calculation"] = "last-number"
		case "table":
			width = 12
			height = 6
			pluginKind = "TimeSeriesChart"
			pluginSpec["legend"] = map[string]any{"position": "right"}
		default: // timeseries
			width = 12
			height = 6
			pluginKind = "TimeSeriesChart"
		}

		// Wrap to next row if this panel doesn't fit.
		if x+width > gridColumns {
			x = 0
			y += rowHeight
			rowHeight = 0
		}

		panels[key] = map[string]any{
			"kind": "Panel",
			"spec": map[string]any{
				"display": map[string]any{"name": p.Title},
				"plugin": map[string]any{
					"kind": pluginKind,
					"spec": pluginSpec,
				},
				"queries": []any{
					map[string]any{
						"kind": "TimeSeriesQuery",
						"spec": map[string]any{
							"plugin": map[string]any{
								"kind": "PrometheusTimeSeriesQuery",
								"spec": map[string]any{
									"query": p.Query,
								},
							},
						},
					},
				},
			},
		}

		items = append(items, map[string]any{
			"x": x, "y": y, "width": width, "height": height,
			"content": map[string]any{"$ref": "#/spec/panels/" + key},
		})

		x += width
		if height > rowHeight {
			rowHeight = height
		}
	}

	return map[string]any{
		"kind": "Dashboard",
		"metadata": map[string]any{
			"name":      slugify(d.Name),
			"project":   project,
			"createdAt": now.Format(time.RFC3339Nano),
			"updatedAt": now.Format(time.RFC3339Nano),
			"version":   0,
		},
		"spec": map[string]any{
			"display":         map[string]any{"name": d.Name},
			"duration":        "30m",
			"refreshInterval": "30s",
			"panels":          panels,
			"layouts": []any{
				map[string]any{
					"kind": "Grid",
					"spec": map[string]any{
						"display": map[string]any{
							"title":    d.Name,
							"collapse": map[string]any{"open": true},
						},
						"items": items,
					},
				},
			},
		},
	}
}

// panelKey generates a valid Perses panel key from the index and title.
// Must match ^[a-zA-Z0-9_.-]+$ and be ≤75 chars.
func panelKey(index int, title string) string {
	s := strings.ToLower(title)
	s = strings.ReplaceAll(s, " ", "_")
	s = nonAlphaNum.ReplaceAllString(s, "_")
	s = multiSep.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 60 {
		s = s[:60]
	}
	return fmt.Sprintf("%s_%d", s, index)
}

var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
var multiSep = regexp.MustCompile(`[_.-]{2,}`)

// slugify converts a display name to a valid Perses resource name.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "&", "and")
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = multiSep.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 75 {
		s = s[:75]
	}
	return s
}

// CreateDashboard creates a single dashboard in the Perses file database.
// project is the project slug, name is the display name, panels is the panel list.
// The dashboard is written to dataDir/dashboards/{project}/{slug}.json.
// If overwrite is true, existing dashboards are replaced.
func CreateDashboard(dataDir, project, name string, panels []simplifiedPanel, overwrite bool) error {
	now := time.Now().UTC()

	// Ensure project exists.
	projDir := filepath.Join(dataDir, "projects")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		return err
	}
	projPath := filepath.Join(projDir, project+".json")
	if _, err := os.Stat(projPath); os.IsNotExist(err) {
		proj := buildProject(project, project, now)
		if err := writeJSON(projPath, proj); err != nil {
			return fmt.Errorf("create project: %w", err)
		}
	}

	// Create dashboard.
	dbDir := filepath.Join(dataDir, "dashboards", project)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return err
	}

	d := simplifiedDashboard{Name: name, Panels: panels}
	db := buildDashboard(project, d, now)
	dbSlug := slugify(name)
	dbPath := filepath.Join(dbDir, dbSlug+".json")

	if !overwrite {
		if _, err := os.Stat(dbPath); err == nil {
			return fmt.Errorf("dashboard %q already exists in project %q", name, project)
		}
	}

	return writeJSON(dbPath, db)
}

// SimplifiedPanel is the exported panel type for dynamic dashboard creation.
type SimplifiedPanel = simplifiedPanel

// writeJSON writes JSON to path, creating or overwriting.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, data, 0o600)
}

// writeJSONIfMissing writes JSON to path only if the file doesn't already exist.
// This avoids overwriting user modifications.
func writeJSONIfMissing(path string, v any) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, data, 0o600)
}
